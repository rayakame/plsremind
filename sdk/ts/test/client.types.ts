import type { Client, Delivery, Item, ItemCreatedEvent, List } from "../src/index.js";

declare const client: Client;

client.on("item.created", (event) => {
  const whole: ItemCreatedEvent = event;
  const item: Item = event.data;
  const old: Item | undefined = event.old;
  const nonce: string | null = event.nonce;
  void [whole, item, old, nonce];
});
client.on("list.deleted", (event) => {
  const id: string = event.data.id;
  const old: List | undefined = event.old;
  void [id, old];
});
client.on("delivery.created", async (event) => {
  const delivery: Delivery = event.data;
  // @ts-expect-error a delivery has nothing from before
  void event.old;
  void delivery;
});
client.on("ready", (event) => {
  const users: readonly string[] = event.users;
  void users;
});
// @ts-expect-error unknown event name
client.on("item.exploded", () => {});
