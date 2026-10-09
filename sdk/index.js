// The runtime half of the SDK: openapi-fetch, typed by index.d.ts against the generated schema.
import createFetchClient from "openapi-fetch";

/** Creates a client for the commerce API. Pass `baseUrl`, for example "https://api.rsl-commerce.test". */
export function createClient(options) {
  return createFetchClient(options);
}
