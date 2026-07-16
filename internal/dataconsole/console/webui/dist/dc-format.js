"use strict";

(function () {
  const root = typeof window !== "undefined" ? window : null;
  if (root) root.DC = root.DC || {};

  function esc(s) {
    return String(s).replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]));
  }

  function fmt(v) {
    return v === null || v === undefined ? "\u2205" : typeof v === "object" ? JSON.stringify(v) : String(v);
  }

  function human(n) {
    if (n == null) return "";
    if (n < 1024) return n + " B";
    if (n < 1 << 20) return (n / 1024).toFixed(1) + " KB";
    return (n / (1 << 20)).toFixed(1) + " MB";
  }

  function baseType(t) {
    return (t || "").split(/[:@]/)[0];
  }

  function isTextual(ctype) {
    return /^(text\/|application\/(json|xml|x-yaml|yaml|javascript|x-ndjson))/.test(ctype || "");
  }

  function isImage(ctype) {
    return /^image\//.test(ctype || "");
  }

  function isImageName(name) {
    return /\.(png|jpe?g|gif|webp|bmp|svg|ico|avif)$/i.test(name || "");
  }

  function byteView(bytes) {
    if (bytes == null) return new Uint8Array(0);
    if (typeof ArrayBuffer !== "undefined" && bytes instanceof ArrayBuffer) return new Uint8Array(bytes);
    if (typeof ArrayBuffer !== "undefined" && ArrayBuffer.isView && ArrayBuffer.isView(bytes)) {
      return new Uint8Array(bytes.buffer, bytes.byteOffset, bytes.byteLength);
    }
    return Uint8Array.from(bytes);
  }

  function b64(bytes) {
    const view = byteView(bytes);
    if (typeof Buffer !== "undefined") return Buffer.from(view).toString("base64");
    let s = "";
    view.forEach((b) => (s += String.fromCharCode(b)));
    return btoa(s);
  }

  const api = {
    esc,
    fmt,
    human,
    baseType,
    isTextual,
    isImage,
    isImageName,
    b64,
  };

  if (root) root.DC.format = api;
  if (typeof module !== "undefined") module.exports = api;
})();
