#include <gotel/gotel.hpp>

#include <utility>

namespace gotel {

std::string error::message() const {
  return "Gotel operation failed (status " + std::to_string(static_cast<int>(code)) + ")";
}

void scope::fail(std::string_view type, std::string_view message) noexcept {
  std::string type_copy(type), message_copy(message);
  gotel_error(&native_, type_copy.c_str(), message_copy.c_str());
}

void scope::set(std::string_view key, std::string_view value) noexcept {
  std::string key_copy(key), value_copy(value);
  gotel_attr_string(&native_, key_copy.c_str(), value_copy.c_str());
}

void scope::set(std::string_view key, std::int64_t value) noexcept {
  std::string key_copy(key);
  gotel_attr_int(&native_, key_copy.c_str(), value);
}

std::expected<host, error> host::create(const gotel_options_t &options) {
  gotel_host_t *native = nullptr;
  auto code = gotel_create(&options, &native);
  if (code != OTLP_OK)
    return std::unexpected(error{code});
  return host(native);
}

std::expected<scope, error> host::start(std::size_t operation, const context *parent) noexcept {
  scope result;
  auto value = parent ? parent->native() : gotel_context_t{};
  gotel_begin(native_, &result.native_, operation, parent ? &value : nullptr);
  result.active_ = result.native_.active;
  return result;
}

} // namespace gotel
