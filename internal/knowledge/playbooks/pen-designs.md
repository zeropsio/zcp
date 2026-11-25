---
description: Reading a person's pen.dev design share (app.pen.dev/s/<token>) — why every fetch tool and the browser fail on it, the two-call route on www.pen.dev that reads it without a sign-in, the script that downloads and unpacks the design and prints a node with its components and variables resolved, and how to draw that node to see it.
---

# Reading a pen.dev design

A person's design is a pen.dev share, `https://app.pen.dev/s/<token>`, opened as "Anyone with a link". The share page is no source for you: a fetch tool and `curl` get an error page (a 500 with Next.js markup that mentions a sign-in, which does not mean the link is private), and `zerops_browser` draws the design on a canvas with no node ids in the page and no way to zoom into a frame. pen.dev's own MCP server talks to its desktop app on the person's machine, so a Mate cannot connect to it. Guessed hosts and paths (`api.pen.dev`, `/api/s/…`, `/shares/…`) answer 404.

**The route** is the editor's own, on `www.pen.dev`, and needs no sign-in:

1. `GET https://www.pen.dev/s/<token>/manifest` is JSON: `rootPath` (the design's file, `<name>.pen`), `previewObjectId`, and `files`, a map from each path to `{id, contentType, sha256}`.
2. `GET https://www.pen.dev/s/<token>/objects/<id>?format=url` is JSON `{url}`, a signed download link. The design file downloads zstd-compressed (`application/zstd`), and `sha256` is the hash of those compressed bytes; an image the design uses (`images/…`) downloads the same way, as it is.

The container has no `zstd` command and Python has no `zstandard`; Node's built-in `zlib.zstdDecompressSync` decodes it. Save this as `/tmp/pen/pen.mjs`:

```js
// pen.mjs fetch <share URL> [images/<file> …] | pen.mjs node <design .json> <node id>
import { createHash } from "node:crypto";
import { mkdir, readFile, rename, writeFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { zstdDecompressSync } from "node:zlib";

const ROOT = "/tmp/pen";
const [command, arg, ...rest] = process.argv.slice(2);

async function fetchShare(share, extra) {
  const token = share.match(/\/s\/([^/?#]+)/)?.[1] ?? share;
  const base = "https://www.pen.dev/s/" + token;
  const get = async (url, what) => {
    const res = await fetch(url);
    if (!res.ok) throw new Error(res.status + " on " + what + ": " + (await res.text()).slice(0, 300));
    return res;
  };
  const manifest = await (await get(base + "/manifest", "manifest")).json();
  for (const path of [manifest.rootPath, ...extra]) {
    const file = manifest.files[path];
    if (!file) throw new Error("not in the manifest: " + path);
    const { url } = await (await get(base + "/objects/" + file.id + "?format=url", "objects (" + path + ")")).json();
    let data = Buffer.from(await (await get(url, "the download of " + path)).arrayBuffer());
    if (createHash("sha256").update(data).digest("hex") !== file.sha256) throw new Error("sha256 differs: " + path);
    let name = path;
    if (file.contentType === "application/zstd") {
      data = zstdDecompressSync(data);
      name += ".json";
    }
    const dest = join(ROOT, token, name);
    await mkdir(dirname(dest), { recursive: true });
    await writeFile(dest + "." + process.pid, data);
    await rename(dest + "." + process.pid, dest);
    console.log(dest, data.length);
  }
}

// One node with what it points at filled in: "$name" values from the file's variables, and each
// ref node replaced by its component (the variant it names), with its own fields and descendants' overrides.
async function printNode(path, id) {
  const design = JSON.parse(await readFile(path, "utf8"));
  const vars = design.variables ?? {};
  const byId = new Map();
  const index = (n) => (byId.set(n.id, n), (n.children ?? []).forEach(index));
  design.children.forEach(index);
  const matches = (n, variant) =>
    n.reusable && typeof n.reusable === "object" && Object.entries(variant).every(([k, v]) => n.reusable[k] === v);
  const find = (n, test) => (test(n) ? n : (n.children ?? []).map((c) => find(c, test)).find(Boolean));
  const value = (v) =>
    typeof v === "string" && v.startsWith("$") && vars[v.slice(1)]
      ? vars[v.slice(1)].value
      : Array.isArray(v)
        ? v.map(value)
        : v && typeof v === "object"
          ? Object.fromEntries(Object.entries(v).map(([k, x]) => [k, value(x)]))
          : v;
  const resolve = (n, overrides = {}) => {
    let node = { ...n, ...overrides[n.id] };
    if (node.type === "ref") {
      const target = byId.get(node.ref);
      if (!target) return { ...node, unresolved: "ref " + node.ref + " is not in this file" };
      const component = target.type === "variants" ? (find(target, (c) => matches(c, node.variant ?? {})) ?? target) : target;
      const { type, ref, variant, descendants, ...own } = node;
      return { ...resolve(component, { ...overrides, ...descendants }), ...value(own), component: component.name };
    }
    const { children, ...fields } = node;
    return { ...value(fields), ...(children && { children: children.map((c) => resolve(c, overrides)) }) };
  };
  const node = byId.get(id);
  if (!node) throw new Error("no node " + id + " in " + path);
  console.log(JSON.stringify(resolve(node), null, 1));
}

if (command === "fetch" && arg) await fetchShare(arg, rest);
else if (command === "node" && arg && rest[0]) await printNode(arg, rest[0]);
else {
  console.error("usage: node pen.mjs fetch <share URL> [images/<file> …]\n       node pen.mjs node <design .json> <node id>");
  process.exit(2);
}
```

- `node /tmp/pen/pen.mjs fetch <share URL> [images/<file> …]` writes the design as `/tmp/pen/<token>/<rootPath>.json` and each named image beside it under its path, and stops on any non-200 or a `sha256` that differs from the manifest.
- `node /tmp/pen/pen.mjs node /tmp/pen/<token>/<rootPath>.json <node id>` prints one node's subtree, never the whole file (it runs to megabytes), with what it points at filled in.

The design file is JSON `{"version", "children": […], "variables": {…}}`:

- **Nodes.** `children` is a tree of nodes with `type` (`frame`, `text`, `icon`, `rectangle`, `ellipse`, `path`, `group`, `ref`, …), `id`, `name` and the fields of their look: `x`, `y`, `width`, `height`, `fill`, `stroke`, `effect`, `cornerRadius`, `layout`, `gap`, `padding`, `alignItems`, `justifyContent`, the font fields and `content` of a text. The "Node ID: ab12c" a person pastes is a node's `id`.
- **Variables.** `variables` holds the design's tokens, `{"<name>": {"type": "color", "value": "#…"}}`; a field written `"$<name>"` takes that token's `value`.
- **Components.** A node with `reusable` is a component. A `ref` node is an instance of one: `ref` is the component's `id` in the same tree; when that is a `variants` node, the instance is its descendant whose `reusable` matches the instance's `variant`; the instance's own fields (`id`, `x`, `y`, `width`, …) win, and `descendants` maps an id inside the component to the fields that differ there (a label's `content`, an icon's name).
- **Images and icons.** A `fill` is a colour, a token, a gradient object, an image `{"type": "image", "url": "images/…"}` (a manifest file: name it to `fetch`), or a list of these. An `icon` node with `library: "lucide"` is the Lucide icon its `icon` names.

`node` resolves the tokens and components; read its output, not the raw file. To see a node's look, write that output as a small HTML page of the node's width and open it in `zerops_browser` at that width: a frame lays its children in a row, `layout: "vertical"` in a column, `layout: "none"` at their `x` and `y`, with `gap`, `padding`, `alignItems` and `justifyContent` as in flexbox; `fill_container` fills the parent and `fit_content` fits the children; a node with `enabled: false` is not drawn. The route `https://www.pen.dev/s/<token>/preview?v=2` gives an image of the whole file, too coarse to build from.

The person edits the share, so download again before relying on a copy from an earlier turn, and read node ids from the copy the person's id belongs to.

The route is not documented by pen.dev and may change. When a call answers anything but 200, tell the person the status and the call that failed; do not try other hosts or paths, and do not ask for a pen.dev account or key. When `manifest` answers 200 and `objects` refuses, the share may not allow downloads: say so, and that the design's owner turns on "Can download .pen file" in the share's settings.
