import createClient from "openapi-fetch";
import type { components, paths } from "./schema";

// Every path, parameter, body and response below is typed from the OpenAPI
// document Bridgr generates; `bun run gen` refreshes ./schema.d.ts.
export const api = createClient<paths>({ baseUrl: window.location.origin });

export type Todo = components["schemas"]["Todo"];
export type TodoInput = components["schemas"]["TodoInput"];
export type Problem = components["schemas"]["Problem"];
