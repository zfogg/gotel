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
  return gotel_command_args_init_ex(out, application, command, argc, argv, true);
}

otlp_status_t gotel_command_args_init_ex(gotel_command_args_t *out, const char *application, const char *command,
                                         int argc, const char *const *argv, bool sanitize) {
  gotel_command_policy_t policy = {.mode = sanitize ? GOTEL_COMMAND_REDACT_ALL : GOTEL_COMMAND_REDACT_NONE};
  return gotel_command_args_init_policy(out, application, command, argc, argv, &policy);
}

static const gotel_command_rule_t *match_rule(const gotel_command_policy_t *policy, const char *arg, size_t *prefix) {
  for (size_t j = 0; j < policy->rule_count; ++j) {
    const gotel_command_rule_t *rule = &policy->rules[j];
    size_t n = strlen(rule->flag);
    if (!strncmp(arg, rule->flag, n) &&
        (!arg[n] || (rule->value != GOTEL_COMMAND_VALUE_NONE && (arg[n] == '=' || (n == 2 && rule->flag[1] != '-'))))) {
      *prefix = n;
      return rule;
    }
  }
  // Conservatively recognize sensitive short options within a cluster. Removing
  // such a rule removes the whole token, including preceding switches.
  if (arg[0] == '-' && arg[1] && arg[1] != '-') {
    for (size_t i = 1; arg[i]; ++i) {
      for (size_t j = 0; j < policy->rule_count; ++j) {
        const gotel_command_rule_t *rule = &policy->rules[j];
        if (strlen(rule->flag) == 2 && rule->flag[1] == arg[i] && rule->value != GOTEL_COMMAND_VALUE_NONE) {
          *prefix = i + 1;
          return rule;
        }
      }
    }
  }
  return NULL;
}

otlp_status_t gotel_command_args_init_policy(gotel_command_args_t *out, const char *application, const char *command,
                                             int argc, const char *const *argv, const gotel_command_policy_t *policy) {
  if (!out || !application || !*application || argc < 0 || argc > 4096 || (argc && !argv))
    return OTLP_ERR_INVALID_ARGUMENT;
  if (!policy || policy->mode < GOTEL_COMMAND_REDACT_NONE || policy->mode > GOTEL_COMMAND_REDACT_ALL ||
      (policy->rule_count && !policy->rules) || (policy->sensitive_value_count && !policy->sensitive_values))
    return OTLP_ERR_INVALID_ARGUMENT;
  for (size_t j = 0; j < policy->rule_count; ++j) {
    const gotel_command_rule_t *r = &policy->rules[j];
    if (!r->flag || r->flag[0] != '-' || !r->flag[1] || !strcmp(r->flag, "--") || r->value < GOTEL_COMMAND_VALUE_NONE ||
        r->value > GOTEL_COMMAND_VALUE_OPTIONAL || r->action < GOTEL_COMMAND_REDACT || r->action > GOTEL_COMMAND_REMOVE)
      return OTLP_ERR_INVALID_ARGUMENT;
  }
  for (size_t j = 0; j < policy->sensitive_value_count; ++j)
    if (!policy->sensitive_values[j])
      return OTLP_ERR_INVALID_ARGUMENT;
  for (int i = 0; i < argc; ++i)
    if (!argv[i])
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
  bool end_options = false, redact_next = false, remove_next = false, consume_next = false;
  for (int i = 0; i < argc; ++i) {
    const char *text = argv[i];
    size_t length = strlen(text), prefix = 0;
    const gotel_command_rule_t *rule = NULL;
    bool redact = redact_next, remove = remove_next;
    bool consumed = consume_next;
    consume_next = false;
    redact_next = remove_next = false;
    if (!consumed && !redact && !remove && i && !end_options) {
      if (!strcmp(text, "--"))
        end_options = true;
      else
        rule = match_rule(policy, text, &prefix);
    }
    if (rule) {
      remove = rule->action == GOTEL_COMMAND_REMOVE;
      redact = policy->mode != GOTEL_COMMAND_REDACT_NONE && rule->action == GOTEL_COMMAND_REDACT;
      bool separate = !text[prefix] && rule->value != GOTEL_COMMAND_VALUE_NONE && i + 1 < argc &&
                      (rule->value == GOTEL_COMMAND_VALUE_REQUIRED || argv[i + 1][0] != '-');
      if (separate) {
        consume_next = true;
        remove_next = remove;
        redact_next = redact;
      }
      if (redact && text[prefix])
        length = prefix;
      // Keep the flag itself when its value is a separate argument.
      redact = false;
    } else if (redact) {
      text = "[REDACTED]";
      length = strlen(text);
    } else if (policy->mode == GOTEL_COMMAND_REDACT_ALL && !remove) {
      text = "[REDACTED]";
      length = strlen(text);
      if (!i) {
        text = application;
        length = app_length;
      } else if (command && !strcmp(argv[i], command)) {
        text = command;
        length = command_length;
      } else if (!end_options && !strncmp(argv[i], "--", 2)) {
        text = argv[i];
        length = strcspn(text, "=");
      }
    }
    if (i && !rule && !remove && policy->mode != GOTEL_COMMAND_REDACT_NONE) {
      for (size_t j = 0; j < policy->sensitive_value_count; ++j) {
        if (*policy->sensitive_values[j] && !strcmp(argv[i], policy->sensitive_values[j])) {
          text = "[REDACTED]";
          length = strlen(text);
          break;
        }
      }
    }
    if (remove)
      continue;
    if (length > 1048576 - total) {
      result = OTLP_ERR_OVERFLOW;
      goto fail;
    }
    total += length;
    args.argv[args.argc] = copy_text(text, length);
    if (!args.argv[args.argc]) {
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
