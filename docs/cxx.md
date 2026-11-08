/** @mainpage Gotel C++ API

Gotel's C++ facade is available from `<gotel/gotel.hpp>` and the CMake target
`gotel::cxx`. It wraps the C lifecycle with C++27 RAII and
`std::expected<T, gotel::error>` results.

@section cxx_intro Usage

@code{.cpp}
#include <gotel/gotel.hpp>

auto host = gotel::host::create(options);
if (!host) {
    std::cerr << host.error().message();
    return;
}

auto span = host->start(operation_index);
if (span) {
    span->set("app.component", "worker");
    span->success();
}
@endcode

@section cxx_ownership Ownership

`gotel::host` owns the native host and destroys it automatically. Scopes are
move-only RAII values: an active scope ends when it is destroyed, while
`end()` and `reset()` allow explicit completion. `gotel::context` is copied by
value and can cross queue or thread boundaries without exposing live span
pointers.

@section cxx_surface API surface

The C++ facade covers host creation, scope parenting, attributes, outcomes,
context propagation, ticking, and draining. C tracer adapters remain optional
targets and use the same core API.
*/
