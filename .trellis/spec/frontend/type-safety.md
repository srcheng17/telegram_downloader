# Runtime Type Safety

Use plain JavaScript and native browser APIs. There is no TypeScript compiler, schema framework or legacy response normalization layer.

Check response objects, lists, optional text and DOM nodes before access. Validate finite numeric progress/size values before formatting. Use FormData/JSON and URL/URLSearchParams rather than manual escaping.

Treat every server/user string as text. Keep backend field names consistent across forms, API and rendering; reject malformed input at the server as well. Browser tests should include empty/error responses and failed submissions without uncaught exceptions.
