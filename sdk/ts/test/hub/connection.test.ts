import { afterEach, expect, test } from "vitest";
import { GatewayError } from "../../src/index.js";
import {
  closeAll,
  connection,
  giveIdentity,
  session,
  tokenClient,
  tokenRest,
  unique,
  until,
} from "./helpers.js";

afterEach(closeAll);

async function linked(provider: string, name: string) {
  const made = await session(name);
  const externalId = unique(name);
  await giveIdentity(made.user.id, provider, externalId);
  return { ...made, externalId };
}

test("a connection sees every linked user and acts for each by id", async () => {
  const provider = unique("chat");
  const mia = await linked(provider, "mia");
  const tom = await linked(provider, "tom");
  await session("unlinked");
  await mia.rest.createItem({ title: "mia's item" });
  const bot = await connection(provider);
  const { client } = tokenClient(bot.token, ["users", "items"]);
  await client.connect();

  expect(client.principal).toMatchObject({
    kind: "connection",
    id: bot.id,
    connection: { provider },
  });
  expect(client.cache.user()).toBeUndefined();
  expect(
    client.cache
      .users()
      .map((user) => user.userId)
      .sort(),
  ).toEqual([mia.user.id, tom.user.id].sort());
  const profile = client.cache.user(mia.user.id)!.profile!;
  expect(profile).toMatchObject({
    displayName: "mia",
    externalIds: [mia.externalId],
    remind: true,
  });
  expect("preferences" in profile).toBe(false);
  expect(
    client.cache
      .user(mia.user.id)!
      .items()
      .map((item) => item.title),
  ).toEqual(["mia's item"]);

  const found = await client.rest.lookupUser(tom.externalId);
  expect(found.id).toBe(tom.user.id);
  const created = await client.rest.createItem({ title: "from the bot" }, { user: tom.user.id });
  await until("the item", () => client.cache.user(tom.user.id)!.item(created.id));
  expect(client.cache.user(mia.user.id)!.item(created.id)).toBeUndefined();
  expect((await tom.rest.getItem(created.id)).title).toBe("from the bot");
});

test("a user who switches the connection off is gone, and comes back when they switch it on", async () => {
  const provider = unique("chat");
  const mia = await linked(provider, "mia");
  const bot = await connection(provider);
  const { client } = tokenClient(bot.token, ["users", "items"]);
  await client.connect();
  const seen: string[] = [];
  client.on("user.gone", (event) => seen.push(`gone ${event.userId}`));
  client.on("user.ready", (event) => seen.push(`ready ${event.userId}`));

  await mia.rest.setConnectionGrant(bot.id, { enabled: false });
  await until("user.gone", () => seen.length === 1);
  expect(client.cache.user(mia.user.id)).toBeUndefined();
  await mia.rest.setConnectionGrant(bot.id, { enabled: true });
  await until("the user again", () => seen.length === 2);
  expect(seen).toEqual([`gone ${mia.user.id}`, `ready ${mia.user.id}`]);
  expect(client.cache.user(mia.user.id)).toBeDefined();
});

test("an intent the admin does not allow ends the client with 4014", async () => {
  const bot = await connection(unique("chat"), ["users", "deliveries"]);
  const { client } = tokenClient(bot.token, ["users", "items"]);
  const error = await client.connect().catch((e: unknown) => e);
  expect(error).toBeInstanceOf(GatewayError);
  expect((error as GatewayError).code).toBe(4014);
  expect(client.status).toBe("closed");
});

test("history without items is refused with 4013", async () => {
  const bot = await connection(unique("chat"));
  const { client } = tokenClient(bot.token, ["history"]);
  await expect(client.connect()).rejects.toMatchObject({ code: 4013 });
});

test("a connection without a user option is refused before the hub is asked", async () => {
  const bot = await connection(unique("chat"));
  await expect(tokenRest(bot.token).listItems()).rejects.toBeInstanceOf(TypeError);
});

test("a connection is told about a reminder with the address to use", async () => {
  const provider = unique("chat");
  const mia = await linked(provider, "mia");
  await mia.rest.updateUser({ locale: "de" });
  const bot = await connection(provider);
  const { client } = tokenClient(bot.token, ["deliveries"]);
  await client.connect();
  const users: unknown[] = [];
  client.on("delivery.created", (event) => users.push(event.data.user));
  const item = await mia.rest.createItem({ title: "ping me" });
  await mia.rest.createReminder(item.id, { at: new Date(Date.now() + 2000) });
  await until("the reminder", () => users.length === 1, 25_000);
  expect(users[0]).toMatchObject({ id: mia.user.id, locale: "de", externalIds: [mia.externalId] });
});
