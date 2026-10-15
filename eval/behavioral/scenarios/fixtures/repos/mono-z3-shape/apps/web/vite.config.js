// Single origin: the browser talks to vite, vite proxies to the server; never
// a baked-in server URL. allowedHosts lets the dev service and *.zerops.app pass.
const server = `http://127.0.0.1:${process.env.DEV_SERVER_PORT ?? 13773}`;
export default {
  server: { allowedHosts: true, proxy: { "/api": server, "/ws": server, "/oauth": server } },
  preview: { allowedHosts: true },
};
