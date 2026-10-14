// Vite answers 403 to hosts it does not know (MODEL S3 g): the dev service and
// *.zerops.app subdomains must pass.
export default {
  server: { allowedHosts: true },
  preview: { allowedHosts: true },
};
