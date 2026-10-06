# Design

## Smallest behavior gap

CPA routes OpenAI chat completions; current extraction only speaks native llama.cpp /props, /apply-template, /tokenize and /completion. Changing a URL cannot bridge these protocols. Keep MiniCPM as the model and add an explicit llama_cpp_chat saved protocol for a verified pinned llama.cpp response transported through CPA. Default absent protocol to llama_cpp_native, and preserve saved protocol when older clients omit it.

## Transport and validation

modelapi owns a chat adapter with a fixed neutral capability probe and bounded complete chat calls. verbose:true exposes the pinned upstream native completion evidence via __verbose. Validate actual pinned system_fingerprint, model, native stop/eos/truncated fields, usage/output counts and configured output budget. Reject length, refusal, tools, missing telemetry, drift and malformed JSON. This verifies untruncated completed responses; it is not native exact preflight token budgeting and does not measure context or template identity. Keep all application typed-field, selected-key, grounding, UTF-16 provenance and CAS checks.

sourcesettings selects the saved protocol and holds the secret snapshot. Protocol is ordinary CAS-protected JSON configuration; no database migration. CLI and frontend preserve/allow the explicit value with Chinese labels and no credential disclosure. Do not silently fall back when a protocol is unsupported.

## Operations

CPA native management config adds a single dedicated MiniCPM OpenAI-compatible provider with the existing upstream key. Reuse its existing client API key for the management workspace. No SSH, no new model public port, no iStore changes. Back up nonsecret source and preserve server-side recovery of secret configuration without exporting keys into new local files. Deploy only required API/static changes via Dockhand with previous source/image retained.

## Field generation order

Live equivalent-schema tests showed this small model copies structural labels into values when grammar generates evidence_quote before value. Serialize each field properties object as value then evidence_quote while preserving all JSON Schema semantics, the existing trusted/user prompts and validator. Both the original title and a different title with internal punctuation passed through CPA. Quotes may include the structural label when their full span is an exact input substring; values must still be semantically correct. No postprocessing or input trimming is introduced.
