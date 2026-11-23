import { readFileSync } from "node:fs";
import { expect, test } from "vitest";
import { parse } from "yaml";
import { RestClient } from "../src/index.js";
import { fakeFetch } from "./fake-fetch.js";

const spec = parse(
  readFileSync(new URL("../../../docs/api/openapi.yaml", import.meta.url), "utf8"),
);
const outside = new Set(["connectGateway", "startOAuth", "finishOAuth"]);
const helpers = new Set(["constructor", "pages", "oauthStartUrl", "identityAvatarUrl"]);

const operations: { id: string; method: string; path: string }[] = [];
for (const [path, entry] of Object.entries<Record<string, { operationId?: string }>>(spec.paths)) {
  for (const method of ["get", "post", "put", "patch", "delete"]) {
    const id = entry[method]?.operationId;
    if (id && !outside.has(id)) operations.push({ id, method: method.toUpperCase(), path });
  }
}

test("every operation of the API has a method of its name, and no method is made up", () => {
  const methods = Object.getOwnPropertyNames(RestClient.prototype).filter(
    (name) => !helpers.has(name),
  );
  expect(methods.sort()).toEqual(operations.map((operation) => operation.id).sort());
});

test.each(operations)("$id asks $method $path", async ({ id, method, path }) => {
  const { fetch, calls } = fakeFetch([{ status: 200, body: {} }]);
  const rest = new RestClient({ url: "https://hub.test", token: "plsr_pat_x", fetch });
  const call = (rest as unknown as Record<string, (...args: unknown[]) => Promise<unknown>>)[id]!;
  await call.call(rest, "ID1", "ID2", {}, {});
  const asked = decodeURIComponent(new URL(calls[0]!.url).pathname).replace("/api/v1", "");
  const expected = path.replace("{user}", "@me");
  const shape = new RegExp(`^${expected.replace(/\{[^}]+\}/g, "(ID1|ID2)")}$`);
  expect(calls[0]!.method).toBe(method);
  expect(asked).toMatch(shape);
});
