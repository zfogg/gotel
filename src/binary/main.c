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
#define isatty _isatty
#define fileno _fileno
#define close _close
#define fdopen _fdopen
#define getpid _getpid
#else
#include <unistd.h>
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

static int regular_file(const char *path, struct stat *info) {
#ifdef _WIN32
  DWORD attributes = GetFileAttributesA(path);
  if (attributes == INVALID_FILE_ATTRIBUTES ||
      (attributes & (FILE_ATTRIBUTE_REPARSE_POINT | FILE_ATTRIBUTE_DIRECTORY)))
    return 0;
  return stat(path, info) == 0 && (info->st_mode & S_IFMT) == S_IFREG;
#else
  return lstat(path, info) == 0 && S_ISREG(info->st_mode);
#endif
}

/* Create an exclusive sibling and finish all writes before replacing the
 * original. Failed formatting or writes never truncate the original file. */
static int replace_file(const char *path, const char *sql, size_t n,
                        struct stat *info) {
  char *temp = malloc(strlen(path) + 64);
  if (!temp)
    return error("out of memory");
  int fd = -1;
  for (unsigned i = 0; i < 100; ++i) {
    snprintf(temp, strlen(path) + 64, "%s.gotel-%ld-%u.tmp", path,
             (long)getpid(), i);
#ifdef _WIN32
    fd = _open(temp, _O_WRONLY | _O_CREAT | _O_EXCL | _O_BINARY,
               _S_IREAD | _S_IWRITE);
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
  if (!failed && _chmod(temp, info->st_mode & (_S_IREAD | _S_IWRITE)) != 0)
    failed = 1;
  if (!failed &&
      !MoveFileExA(temp, path,
                   MOVEFILE_REPLACE_EXISTING | MOVEFILE_WRITE_THROUGH))
    failed = 1;
#else
  if (!failed && rename(temp, path) != 0)
    failed = 1;
#endif
  if (failed)
    remove(temp);
  free(temp);
  return failed ? error("cannot replace file; original retained") : 0;
}

int main(int argc, char **argv) {
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
  struct stat info;
  FILE *input = stdin;
  if (path) {
    if (!regular_file(path, &info))
      return error("--edit-file requires a regular file (no symlinks)");
    input = fopen(path, "rb");
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
