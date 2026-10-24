#if !defined(_WIN32) && !defined(_POSIX_C_SOURCE)
#define _POSIX_C_SOURCE 200809L
#endif
#include "internal.h"
/* The pinned exporter already supports owned ArrayValues internally, while its
 * public resource options accept scalars only. Use that one seam at creation,
 * before the host is published to any producer/exporter thread. */
#include "exporter_internal.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#ifdef _WIN32
#include <windows.h>

#include <shellapi.h>
#else
#include <pwd.h>
#include <sys/utsname.h>
#include <unistd.h>
#ifdef __APPLE__
#include <crt_externs.h>
#include <mach-o/dyld.h>
#endif
#endif

static bool has(gotel_host_t *h, const char *key) {
  struct otlp_attr_vec *v = &h->exporter->resource_attrs;
  for (size_t i = 0; i < v->n; ++i)
    if (!strcmp(v->items[i].key, key))
      return true;
  return false;
}
static otlp_status_t value(gotel_host_t *h, const char *key, otlp_value_t v) {
  if (has(h, key))
    return OTLP_OK;
  return otlp_attr_vec_set(&h->exporter->resource_attrs, SIZE_MAX, key, &v);
}
static otlp_status_t string(gotel_host_t *h, const char *key, const char *s) {
  if (!s || !*s)
    return OTLP_OK;
  return value(h, key, (otlp_value_t){.type = OTLP_VALUE_STRING, .v.string_val = s});
}
static otlp_status_t args(gotel_host_t *h, int argc, const char *const *argv) {
  if (argc < 0 || argc > 4096 || (!argv && argc))
    return OTLP_ERR_INVALID_ARGUMENT;
  if (has(h, "process.command_args"))
    return OTLP_OK;
  otlp_value_t *items = calloc((size_t)argc + 1, sizeof(*items));
  if (!items)
    return OTLP_ERR_NOMEM;
  size_t total = 0;
  otlp_status_t result = OTLP_OK;
  for (int i = 0; i < argc; ++i) {
    if (!argv[i]) {
      result = OTLP_ERR_INVALID_ARGUMENT;
      goto done;
    }
    size_t n = strlen(argv[i]);
    if (n > 1048576 - total) {
      result = OTLP_ERR_OVERFLOW;
      goto done;
    }
    total += n;
    items[i] = (otlp_value_t){.type = OTLP_VALUE_STRING, .v.string_val = argv[i]};
  }
  result = otlp_attr_vec_set_array(&h->exporter->resource_attrs, SIZE_MAX, "process.command_args", items, (size_t)argc);
done:
  free(items);
  return result;
}
#ifdef _WIN32
static bool utf8(const wchar_t *w, char *s, size_t n) {
  return WideCharToMultiByte(CP_UTF8, WC_ERR_INVALID_CHARS, w, -1, s, (int)n, NULL, NULL) > 0;
}
static otlp_status_t native_args(gotel_host_t *h) {
  int argc = 0;
  wchar_t **wide = CommandLineToArgvW(GetCommandLineW(), &argc);
  if (!wide)
    return OTLP_ERR_INVALID_ARGUMENT;
  char **argv = calloc((size_t)argc + 1, sizeof(*argv));
  otlp_status_t result = OTLP_ERR_NOMEM;
  if (!argv) {
    LocalFree(wide);
    return result;
  }
  for (int i = 0; i < argc; ++i) {
    int n = WideCharToMultiByte(CP_UTF8, WC_ERR_INVALID_CHARS, wide[i], -1, NULL, 0, NULL, NULL);
    if (n <= 0) {
      result = OTLP_ERR_INVALID_ARGUMENT;
      goto done;
    }
    argv[i] = malloc((size_t)n);
    if (!argv[i])
      goto done;
    if (!utf8(wide[i], argv[i], (size_t)n)) {
      result = OTLP_ERR_INVALID_ARGUMENT;
      goto done;
    }
  }
  result = args(h, argc, (const char *const *)argv);
done:
  for (int i = 0; i < argc; ++i)
    free(argv[i]);
  free(argv);
  LocalFree(wide);
  return result;
}
#else
static otlp_status_t native_args(gotel_host_t *h) {
#ifdef __APPLE__
  return args(h, *_NSGetArgc(), (const char *const *)*_NSGetArgv());
#elif defined(__linux__)
  FILE *file = fopen("/proc/self/cmdline", "rb");
  if (!file)
    return OTLP_OK;
  size_t cap = 4096, n = 0;
  char *data = malloc(cap);
  if (!data) {
    fclose(file);
    return OTLP_ERR_NOMEM;
  }
  while (!feof(file)) {
    if (n == cap) {
      if (cap == 1048576) {
        free(data);
        fclose(file);
        return OTLP_ERR_OVERFLOW;
      }
      cap *= 2;
      char *p = realloc(data, cap);
      if (!p) {
        free(data);
        fclose(file);
        return OTLP_ERR_NOMEM;
      }
      data = p;
    }
    size_t got = fread(data + n, 1, cap - n, file);
    n += got;
    if (!got)
      break;
  }
  bool failed = ferror(file) != 0;
  fclose(file);
  if (failed || !n || data[n - 1]) {
    free(data);
    return OTLP_OK;
  }
  const char **argv = calloc(4096, sizeof(*argv));
  if (!argv) {
    free(data);
    return OTLP_ERR_NOMEM;
  }
  int argc = 0;
  otlp_status_t result = OTLP_OK;
  for (size_t i = 0; i < n;) {
    if (argc == 4096) {
      result = OTLP_ERR_OVERFLOW;
      goto done;
    }
    argv[argc++] = data + i;
    i += strlen(data + i) + 1;
  }
  result = args(h, argc, argv);
done:
  free(argv);
  free(data);
  return result;
#else
  (void)h;
  return OTLP_OK;
#endif
}
#endif
#define TRY(call)                                                                                                      \
  do {                                                                                                                 \
    otlp_status_t status = (call);                                                                                     \
    if (status != OTLP_OK)                                                                                             \
      return status;                                                                                                   \
  } while (0)
otlp_status_t gotel_resource_detect(gotel_host_t *h) {
  if (h->options.disable_resource_detection)
    return OTLP_OK;
  TRY(string(h, "telemetry.sdk.name", "c"));
  TRY(string(h, "telemetry.sdk.language", "c"));
  TRY(string(h, "telemetry.sdk.version", GOTEL_VERSION));
  TRY(string(h, "process.runtime.name", "C"));
#ifdef __clang__
  TRY(string(h, "process.runtime.version", __clang_version__));
  TRY(string(h, "process.runtime.description", "Clang " __clang_version__));
#elif defined(__GNUC__)
  TRY(string(h, "process.runtime.version", __VERSION__));
  TRY(string(h, "process.runtime.description", "GCC " __VERSION__));
#elif defined(_MSC_VER)
  TRY(string(h, "process.runtime.description", "Microsoft C"));
#endif
  char executable[32768] = {0}, host[1024] = {0}, owner[1024] = {0}, description[2048] = {0};
#ifdef _WIN32
  wchar_t wide[32768];
  DWORD count = 32768;
  DWORD length = GetModuleFileNameW(NULL, wide, 32768);
  if (length && length < 32768)
    utf8(wide, executable, sizeof(executable));
  count = 32768;
  if (GetComputerNameExW(ComputerNameDnsHostname, wide, &count))
    utf8(wide, host, sizeof(host));
  count = 32768;
  if (GetUserNameW(wide, &count))
    utf8(wide, owner, sizeof(owner));
  typedef LONG(WINAPI * version_fn)(OSVERSIONINFOW *);
  version_fn version = (version_fn)(void *)GetProcAddress(GetModuleHandleW(L"ntdll.dll"), "RtlGetVersion");
  OSVERSIONINFOW info = {0};
  info.dwOSVersionInfoSize = sizeof(info);
  if (version && version(&info) == 0)
    snprintf(description, sizeof(description), "Windows %lu.%lu build %lu", (unsigned long)info.dwMajorVersion,
             (unsigned long)info.dwMinorVersion, (unsigned long)info.dwBuildNumber);
  TRY(string(h, "os.type", "windows"));
  TRY(value(h, "process.pid", (otlp_value_t){.type = OTLP_VALUE_INT64, .v.int64_val = (int64_t)GetCurrentProcessId()}));
#else
  gethostname(host, sizeof(host) - 1);
  struct utsname info;
  if (uname(&info) == 0) {
    snprintf(description, sizeof(description), "%s %s %s %s", info.sysname, info.release, info.version, info.machine);
    TRY(string(h, "host.arch", info.machine));
  }
  struct passwd pw, *found = NULL;
  char buffer[16384];
  if (getpwuid_r(geteuid(), &pw, buffer, sizeof(buffer), &found) == 0 && found)
    snprintf(owner, sizeof(owner), "%s", pw.pw_name);
  TRY(value(h, "process.pid", (otlp_value_t){.type = OTLP_VALUE_INT64, .v.int64_val = (int64_t)getpid()}));
#ifdef __linux__
  ssize_t length = readlink("/proc/self/exe", executable, sizeof(executable) - 1);
  if (length > 0)
    executable[length] = 0;
  TRY(string(h, "os.type", "linux"));
#elif defined(__APPLE__)
  uint32_t size = sizeof(executable);
  if (_NSGetExecutablePath(executable, &size) != 0)
    *executable = 0;
  TRY(string(h, "os.type", "darwin"));
#else
  TRY(string(h, "os.type", "unknown"));
#endif
#endif
#if defined(_M_X64) || defined(__x86_64__)
  TRY(string(h, "host.arch", "amd64"));
#elif defined(_M_ARM64) || defined(__aarch64__)
  TRY(string(h, "host.arch", "arm64"));
#elif defined(_M_IX86) || defined(__i386__)
  TRY(string(h, "host.arch", "386"));
#endif
  TRY(string(h, "host.name", host));
  TRY(string(h, "os.description", description));
  TRY(string(h, "process.owner", owner));
  TRY(string(h, "process.executable.path", executable));
  const char *base = executable;
  for (const char *p = executable; *p; ++p)
    if (*p == '/' || *p == '\\')
      base = p + 1;
  TRY(string(h, "process.executable.name", base));
  if (!h->options.omit_command_args) {
    if (h->options.argv)
      TRY(args(h, h->options.argc, h->options.argv));
    else if (h->options.argc)
      return OTLP_ERR_INVALID_ARGUMENT;
    else
      TRY(native_args(h));
  }
  return OTLP_OK;
}
