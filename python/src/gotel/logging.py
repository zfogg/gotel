"""Correlation processor for structlog; does not configure application rendering."""

from .application import correlation


def add_trace_context(logger, method_name, event_dict):
    event_dict.update(correlation())
    return event_dict
