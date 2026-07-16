"use strict";

const assert = require("assert");
const format = require("../dist/dc-format");

assert.strictEqual(
  format.esc('<tag a="&">'),
  "&lt;tag a=&quot;&amp;&quot;&gt;",
  "esc escapes HTML-sensitive characters used by the SPA"
);
assert.strictEqual(format.esc(null), "null", "esc stringifies non-string inputs");

assert.strictEqual(format.human(null), "", "null sizes render empty");
assert.strictEqual(format.human(1023), "1023 B", "bytes stay in B below 1 KiB");
assert.strictEqual(format.human(1024), "1.0 KB", "1 KiB renders as KB");
assert.strictEqual(format.human(1048576), "1.0 MB", "1 MiB renders as MB");

assert.strictEqual(format.baseType("postgresql:single@18"), "postgresql", "baseType strips variant and version");
assert.strictEqual(format.baseType("nodejs@22"), "nodejs", "baseType strips version");
assert.strictEqual(format.baseType(""), "", "empty baseType stays empty");

assert.strictEqual(format.fmt(null), "\u2205", "null table cells render as empty-set marker");
assert.strictEqual(format.fmt(undefined), "\u2205", "undefined table cells render as empty-set marker");
assert.strictEqual(format.fmt({ a: 1 }), '{"a":1}', "objects render as JSON");
assert.strictEqual(format.fmt(42), "42", "numbers stringify");

assert.strictEqual(format.isTextual("text/plain; charset=utf-8"), true, "text/* is textual");
assert.strictEqual(format.isTextual("application/json"), true, "application/json is textual");
assert.strictEqual(format.isTextual("application/octet-stream"), false, "octet-stream is binary");
assert.strictEqual(format.isImage("image/png"), true, "image/* is image");
assert.strictEqual(format.isImage("application/svg+xml"), false, "non-image content type is not image");
assert.strictEqual(format.isImageName("avatar.WEBP"), true, "known image extensions are image names");
assert.strictEqual(format.isImageName("archive.tar.gz"), false, "unknown extensions are not image names");

assert.strictEqual(format.b64(Uint8Array.from([104, 105])), "aGk=", "b64 encodes bytes in node");

console.log("dc-format.test.js OK");
