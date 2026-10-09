import type { Client, ClientOptions } from "openapi-fetch";
import type { components, paths } from "./schema.js";

export type { components, paths };

/** A product as customers see it. Display its price; never compute one. */
export type Product = components["schemas"]["Product"];
/** An amount in the currency's minor units (cents for USD). */
export type Money = components["schemas"]["Money"];
/** One page of products; follow `next_cursor` until it is null. */
export type ProductPage = components["schemas"]["ProductPage"];

/** Creates a client for the commerce API. Pass `baseUrl`, for example "https://api.rsl-commerce.test". */
export declare function createClient(options?: ClientOptions): Client<paths>;
