#define _POSIX_C_SOURCE 200809L
#include <errno.h>
#include <fcntl.h>
#include <gotel/sql.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#ifdef _WIN32
#include <io.h>
#include <process.h>
#include <windows.h>
/* shellapi requires the Windows base types above. */
#include <shellapi.h>
#define isatty _isatty
#define fileno _fileno
#define close _close
#define fdopen _fdopen
#define getpid _getpid
#else
#include <unistd.h>
#endif

#ifdef _WIN32
typedef struct _stat file_info;
#else
typedef struct stat file_info;
#endif

#define MAX_SQL (1024 * 1024)
int edit_sql(const char *, int, char *, size_t);
static const char usage[] =
    "Usage:\n"
    "  gotel sql format [--dialect DIALECT] [SQL]\n"
    "  gotel sql format [--dialect DIALECT] --edit-file PATH\n"
    "  gotel sql sanitize [--dialect DIALECT] [SQL]\n\n"
    "SQL may be supplied as one argument or piped on stdin.\n"
    "Dialects: generic, postgresql, sqlite, mysql, duckdb, clickhouse.\n"
    "--edit-file preserves newlines, comments, literals and indentation.\n";

static int error(const char *message) {
  fprintf(stderr, "gotel: %s\n", message);
  return 1;
}

#ifdef _WIN32
/* Internally argv and paths are UTF-8; Windows APIs receive UTF-16. */
static wchar_t *wide_path(const char *path) {
  int n = MultiByteToWideChar(CP_UTF8, MB_ERR_INVALID_CHARS, path, -1, NULL, 0);
  wchar_t *wide = n ? malloc((size_t)n * sizeof(*wide)) : NULL;
  if (wide &&
      !MultiByteToWideChar(CP_UTF8, MB_ERR_INVALID_CHARS, path, -1, wide, n)) {
    free(wide);
    return NULL;
  }
  return wide;
}
static FILE *open_input(const char *path) {
  wchar_t *wide = wide_path(path);
  FILE *file = wide ? _wfopen(wide, L"rb") : NULL;
  free(wide);
  return file;
}
static int create_temp(const char *path) {
  wchar_t *wide = wide_path(path);
  int fd = wide ? _wopen(wide, _O_WRONLY | _O_CREAT | _O_EXCL | _O_BINARY,
                         _S_IREAD | _S_IWRITE)
                : -1;
  free(wide);
  return fd;
}
static int finish_temp(const char *temp, const char *path, file_info *info,
                       int failed) {
  wchar_t *from = wide_path(temp), *to = wide_path(path);
  if (!from || !to)
    failed = 1;
  if (!failed && _wchmod(from, info->st_mode & (_S_IREAD | _S_IWRITE)))
    failed = 1;
  if (!failed &&
      !MoveFileExW(from, to,
                   MOVEFILE_REPLACE_EXISTING | MOVEFILE_WRITE_THROUGH))
    failed = 1;
  if (failed && from) {
    /* A read-only original can reject replacement after chmod of the sibling.
     * Restore write access before removing that sibling on the error path. */
    _wchmod(from, _S_IREAD | _S_IWRITE);
    _wremove(from);
  }
  free(from);
  free(to);
  return failed;
}
#else
static FILE *open_input(const char *path) { return fopen(path, "rb"); }
#endif

static int regular_file(const char *path, file_info *info) {
#ifdef _WIN32
  wchar_t *wide = wide_path(path);
  if (!wide)
    return 0;
  DWORD attributes = GetFileAttributesW(wide);
  int status = _wstat(wide, info);
  free(wide);
  if (attributes == INVALID_FILE_ATTRIBUTES ||
      (attributes & (FILE_ATTRIBUTE_REPARSE_POINT | FILE_ATTRIBUTE_DIRECTORY)))
    return 0;
  return status == 0 && (info->st_mode & S_IFMT) == S_IFREG;
#else
  return lstat(path, info) == 0 && S_ISREG(info->st_mode);
#endif
}

/* Create an exclusive sibling and finish all writes before replacing the
 * original. Failed formatting or writes never truncate the original file. */
static int replace_file(const char *path, const char *sql, size_t n,
                        file_info *info) {
  char *temp = malloc(strlen(path) + 64);
  if (!temp)
    return error("out of memory");
  int fd = -1;
  for (unsigned i = 0; i < 100; ++i) {
    snprintf(temp, strlen(path) + 64, "%s.gotel-%ld-%u.tmp", path,
             (long)getpid(), i);
#ifdef _WIN32
    fd = create_temp(temp);
#else
    fd = open(temp, O_WRONLY | O_CREAT | O_EXCL, 0600);
#endif
    if (fd >= 0 || errno != EEXIST)
      break;
  }
  if (fd < 0) {
    free(temp);
    return error("cannot create temporary sibling");
  }
  FILE *file = fdopen(fd, "wb");
  int failed = 0;
  if (!file) {
    close(fd);
    failed = 1;
  } else {
    failed = fwrite(sql, 1, n, file) != n || fflush(file) != 0;
#ifdef _WIN32
    if (!failed && _commit(fd) != 0)
      failed = 1;
#else
    if (!failed && (fchmod(fd, info->st_mode & 0777) != 0 || fsync(fd) != 0))
      failed = 1;
#endif
    if (fclose(file) != 0)
      failed = 1;
  }
#ifdef _WIN32
  failed = finish_temp(temp, path, info, failed);
#else
  if (!failed && rename(temp, path) != 0)
    failed = 1;
#endif
#ifndef _WIN32
  if (failed)
    remove(temp);
#endif
  free(temp);
  return failed ? error("cannot replace file; original retained") : 0;
}

static int run(int argc, char **argv) {
  if (argc == 2 && (!strcmp(argv[1], "--help") || !strcmp(argv[1], "-h"))) {
    fputs(usage, stdout);
    return 0;
  }
  if (argc < 3 || strcmp(argv[1], "sql") ||
      (strcmp(argv[2], "format") && strcmp(argv[2], "sanitize")))
    return error(usage);
  int format = !strcmp(argv[2], "format");
  const char *dialect = "generic", *path = NULL, *argument = NULL;
  int options = 1;
  for (int i = 3; i < argc; ++i) {
    const char *arg = argv[i];
    if (options && (!strcmp(arg, "--help") || !strcmp(arg, "-h"))) {
      fputs(usage, stdout);
      return 0;
    }
    if (options && !strcmp(arg, "--")) {
      options = 0;
      continue;
    }
    if (options && (!strcmp(arg, "--dialect") || !strcmp(arg, "--edit-file"))) {
      if (++i == argc || !*argv[i])
        return error("option requires a value");
      if (!strcmp(arg, "--dialect"))
        dialect = argv[i];
      else if (!format)
        return error("--edit-file requires sql format");
      else
        path = argv[i];
    } else if (options && !strncmp(arg, "--dialect=", 10))
      dialect = arg + 10;
    else if (options && !strncmp(arg, "--edit-file=", 12) && format && arg[12])
      path = arg + 12;
    else if (options && arg[0] == '-')
      return error("unknown option; use -- before SQL starting with '-'");
    else if (argument)
      return error("provide one SQL argument");
    else
      argument = arg;
  }
  if (path && argument)
    return error("provide SQL or --edit-file, not both");
  const char *names[] = {"generic", "sqlite", "postgresql",
                         "duckdb",  "mysql",  "clickhouse"};
  int d;
  for (d = 0; d < 6 && strcmp(dialect, names[d]); ++d) {
  }
  if (d == 6)
    return error("unsupported SQL dialect");
  file_info info;
  FILE *input = stdin;
  if (path) {
    if (!regular_file(path, &info))
      return error("--edit-file requires a regular file (no symlinks)");
    input = open_input(path);
    if (!input)
      return error("cannot open file");
  } else if (!argument && isatty(fileno(stdin)))
    return error(usage);
#ifdef _WIN32
  if (!path && !argument)
    _setmode(fileno(stdin), _O_BINARY);
  _setmode(fileno(stdout), _O_BINARY);
#endif
  char *sql = calloc(MAX_SQL + 2, 1);
  if (!sql) {
    if (path)
      fclose(input);
    return error("out of memory");
  }
  size_t n;
  int failed = 0;
  if (argument) {
    n = strlen(argument);
    if (n <= MAX_SQL)
      memcpy(sql, argument, n);
  } else {
    n = fread(sql, 1, MAX_SQL + 1, input);
    failed = ferror(input);
    if (path && fclose(input) != 0)
      failed = 1;
  }
  if (failed || n > MAX_SQL || memchr(sql, 0, n)) {
    free(sql);
    return error("input read error, NUL or SQL exceeds 1 MiB");
  }
  size_t capacity = n + 2, length = 0;
  if (format && !path) {
    if (gotel_sql_format(sql, (gotel_sql_dialect_t)d, NULL, 0, &length) !=
        OTLP_OK) {
      free(sql);
      return error("invalid or unsupported SQL");
    }
    capacity = length + 1;
  }
  char *out = calloc(capacity, 1);
  if (!out) {
    free(sql);
    return error("out of memory");
  }
  if (path)
    failed = edit_sql(sql, d, out, capacity);
  else if (format)
    failed = gotel_sql_format(sql, (gotel_sql_dialect_t)d, out, capacity,
                              &length) != OTLP_OK;
  else {
    bool truncated = false;
    failed = gotel_sql_sanitize(sql, (gotel_sql_dialect_t)d, true, capacity - 1,
                                out, capacity, &truncated) != OTLP_OK;
    failed |= truncated;
  }
  int result;
  if (failed)
    result = error("invalid or unsupported SQL");
  else if (path)
    result = strcmp(sql, out) ? replace_file(path, out, strlen(out), &info) : 0;
  else
    result = fwrite(out, 1, strlen(out), stdout) != strlen(out) ||
                     fflush(stdout) != 0
                 ? error("cannot write stdout")
                 : 0;
  free(sql);
  free(out);
  return result;
}

int main(int argc, char **argv) {
#ifdef _WIN32
  wchar_t **wide = CommandLineToArgvW(GetCommandLineW(), &argc);
  if (!wide)
    return error("cannot read Unicode command line");
  char **utf8 = calloc((size_t)argc + 1, sizeof(*utf8));
  int failed = !utf8;
  for (int i = 0; !failed && i < argc; ++i) {
    int n = WideCharToMultiByte(CP_UTF8, WC_ERR_INVALID_CHARS, wide[i], -1,
                                NULL, 0, NULL, NULL);
    utf8[i] = n ? malloc((size_t)n) : NULL;
    if (!utf8[i] || !WideCharToMultiByte(CP_UTF8, WC_ERR_INVALID_CHARS, wide[i],
                                         -1, utf8[i], n, NULL, NULL))
      failed = 1;
  }
  LocalFree(wide);
  int result =
      failed ? error("cannot decode Unicode command line") : run(argc, utf8);
  if (utf8) {
    for (int i = 0; i < argc; ++i)
      free(utf8[i]);
    free(utf8);
  }
  (void)argv;
  return result;
#else
  return run(argc, argv);
#endif
}
