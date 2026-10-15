// Vite answers 403 to hosts it does not know: the dev service and
// *.zerops.app subdomains must pass; /api goes to the node server.
export default {
  server: { allowedHosts: true, proxy: { "/api": "http://127.0.0.1:3000" } },
  preview: { allowedHosts: true },
};
