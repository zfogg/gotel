#include <gotel/gotel.h>
#include <stdlib.h>
#include <string.h>

static char *copy_text(const char *value, size_t length) {
  char *copy = malloc(length + 1);
  if (copy) {
    memcpy(copy, value, length);
    copy[length] = 0;
  }
  return copy;
}

void gotel_command_args_destroy(gotel_command_args_t *args) {
  if (!args)
    return;
  for (int i = 0; i < args->argc; ++i)
    free((void *)args->argv[i]);
  free(args->argv);
  free((void *)args->application_name);
  free((void *)args->command_name);
  memset(args, 0, sizeof(*args));
}

otlp_status_t gotel_command_args_init(gotel_command_args_t *out, const char *application, const char *command, int argc,
                                      const char *const *argv) {
  if (!out || !application || !*application || argc < 0 || argc > 4096 || (argc && !argv))
    return OTLP_ERR_INVALID_ARGUMENT;
  gotel_command_args_t args = {0};
  size_t app_length = strlen(application), command_length = command ? strlen(command) : 0;
  if (app_length > 1048576 || command_length > 1048576)
    return OTLP_ERR_OVERFLOW;
  args.application_name = copy_text(application, app_length);
  if (command)
    args.command_name = copy_text(command, command_length);
  args.argv = calloc((size_t)argc + 1, sizeof(*args.argv));
  otlp_status_t result = OTLP_ERR_NOMEM;
  if (!args.application_name || (command && !args.command_name) || !args.argv)
    goto fail;
  size_t total = 0;
  for (int i = 0; i < argc; ++i) {
    if (!argv[i]) {
      result = OTLP_ERR_INVALID_ARGUMENT;
      goto fail;
    }
    const char *text = "[REDACTED]";
    size_t length = strlen(text);
    if (!i) {
      text = application;
      length = app_length;
    } else if (command && !strcmp(argv[i], command)) {
      text = command;
      length = command_length;
    } else if (!strncmp(argv[i], "--", 2)) {
      text = argv[i];
      length = strcspn(text, "=");
    }
    if (length > 1048576 - total) {
      result = OTLP_ERR_OVERFLOW;
      goto fail;
    }
    total += length;
    args.argv[i] = copy_text(text, length);
    if (!args.argv[i]) {
      result = OTLP_ERR_NOMEM;
      goto fail;
    }
    ++args.argc;
  }
  *out = args;
  return OTLP_OK;
fail:
  gotel_command_args_destroy(&args);
  return result;
}

otlp_status_t gotel_command_args_set_span(gotel_scope_t *scope, const gotel_command_args_t *args) {
  if (!scope || !args || !args->application_name || args->argc < 0 || args->argc > 4096 || !args->argv)
    return OTLP_ERR_INVALID_ARGUMENT;
  if (!scope->span)
    return OTLP_OK;
  otlp_status_t result = otlp_span_set_attribute_string(scope->span, "process.command", args->application_name);
  if (result != OTLP_OK)
    return result;
  if (args->command_name) {
    result = otlp_span_set_attribute_string(scope->span, "cli.command.name", args->command_name);
    if (result != OTLP_OK)
      return result;
  }
  otlp_value_t *items = calloc((size_t)args->argc + 1, sizeof(*items));
  if (!items)
    return OTLP_ERR_NOMEM;
  for (int i = 0; i < args->argc; ++i) {
    items[i].type = OTLP_VALUE_STRING;
    items[i].v.string_val = args->argv[i];
  }
  result = otlp_span_set_attribute_array(scope->span, "process.command_args", items, (size_t)args->argc);
  free(items);
  return result;
}
