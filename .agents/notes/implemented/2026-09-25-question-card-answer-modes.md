# Question Card Answer Modes

## Context

The shared question card can carry a list of choices, but providers do not
share the same answer contract. Kimi can reject a free-form value when a
pending request permits only listed options. Claude elicitation cards and the
Gemini bridge support additional free-text answers.

## Decision

A question with no options accepts free text. A question with options is
option-only unless its producer sets allow_free_text: true. The Flutter
client preserves an allowed free-form answer as entered and never maps it to a
different option.

## Alternatives

- Always show a text field: this exposes an answer Kimi can reject.
- Hide text fields for every question with options: this breaks Claude and
  Gemini flows that accept additional text.
- Infer support from footer prose: prose is not a stable machine-readable
  contract.

## Consequences

allow_free_text is an optional per-question card field. Generic cards that
list options default to option-only behavior. Producers that support both
choices and free text must declare that capability. Older clients can ignore
the additive field.

## Verification

Focused backend tests cover generic-card normalization and Claude/Gemini
producers. Flutter tests cover option-only input suppression, exact option
submission, arbitrary free-text submission when enabled, and failure retry
state.
