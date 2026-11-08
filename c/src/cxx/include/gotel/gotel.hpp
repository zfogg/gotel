#pragma once

#include <gotel/gotel.h>

#include <cstddef>
#include <cstdint>
#include <expected>
#include <string>
#include <string_view>

namespace gotel {

struct error {
  otlp_status_t code;
  [[nodiscard]] std::string message() const;
};

class context {
public:
  context() = default;
  explicit context(gotel_context_t value) : value_(value) {}
  [[nodiscard]] const gotel_context_t &native() const noexcept { return value_; }
  [[nodiscard]] static context current() noexcept { return context{gotel_current_context()}; }

private:
  gotel_context_t value_{};
};

class scope {
public:
  scope() = default;
  ~scope() { reset(); }
  scope(const scope &) = delete;
  scope &operator=(const scope &) = delete;
  scope(scope &&other) noexcept : native_(other.native_), active_(other.active_) { other.active_ = false; }
  scope &operator=(scope &&other) noexcept {
    if (this != &other) {
      reset();
      native_ = other.native_;
      active_ = other.active_;
      other.active_ = false;
    }
    return *this;
  }
  void reset() noexcept {
    if (active_)
      gotel_end(&native_);
    active_ = false;
  }
  void end() noexcept { reset(); }
  void success() noexcept { gotel_outcome(&native_, GOTEL_OUTCOME_SUCCESS, nullptr); }
  void fail(std::string_view type, std::string_view message) noexcept;
  void set(std::string_view key, std::string_view value) noexcept;
  void set(std::string_view key, std::int64_t value) noexcept;

private:
  friend class host;
  gotel_scope_t native_{};
  bool active_ = false;
};

class host {
public:
  static std::expected<host, error> create(const gotel_options_t &options);
  ~host() {
    if (native_)
      gotel_destroy(native_);
  }
  host(const host &) = delete;
  host &operator=(const host &) = delete;
  host(host &&other) noexcept : native_(other.native_) { other.native_ = nullptr; }
  host &operator=(host &&other) noexcept {
    if (this != &other) {
      if (native_)
        gotel_destroy(native_);
      native_ = other.native_;
      other.native_ = nullptr;
    }
    return *this;
  }
  [[nodiscard]] std::expected<scope, error> start(std::size_t operation, const context *parent = nullptr) noexcept;
  [[nodiscard]] otlp_status_t tick() noexcept { return gotel_tick(native_); }
  [[nodiscard]] otlp_status_t drain() noexcept { return gotel_drain(native_); }

private:
  explicit host(gotel_host_t *native) : native_(native) {}
  gotel_host_t *native_ = nullptr;
};

} // namespace gotel
