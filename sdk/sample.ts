// Type-checked on every pull request against the freshly generated schema (REQ-012). If the API's
// types change shape, this stops compiling; the @ts-expect-error lines fail if the types go loose.
import { createClient, type Product } from "./index.js";

const api = createClient({ baseUrl: "https://api.rsl-commerce.test" });

export async function allProducts(): Promise<Product[]> {
  const products: Product[] = [];
  let cursor: string | undefined;
  do {
    const { data, error } = await api.GET("/v1/products", {
      params: { query: { limit: 100, cursor } },
    });
    if (error) throw new Error(error.detail ?? "request failed");
    products.push(...data.items);
    cursor = data.next_cursor ?? undefined;
  } while (cursor);
  return products;
}

export function label(p: Product): string {
  const amount: number = p.price.amount;
  const currency: string = p.price.currency;
  return `${p.name}: ${amount} ${currency}`;
}

export async function health(): Promise<"ok" | "unavailable" | undefined> {
  const { data } = await api.GET("/health");
  return data?.status;
}

// @ts-expect-error: the page's field is `items`, not `item`
export const wrongField = (page: import("./index.js").ProductPage) => page.item;

// @ts-expect-error: no such path
export const wrongPath = () => api.GET("/v1/product");
