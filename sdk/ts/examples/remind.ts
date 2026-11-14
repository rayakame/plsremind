// A minimal reminder adapter: it prints every reminder it is given and confirms it.
// How to run it is in the README.
import { ApiError, Client, formatDuration, type Delivery } from "plsremind";

const url = process.env.PLSREMIND_URL;
const token = process.env.PLSREMIND_TOKEN;
if (!url || !token) {
  console.error("set PLSREMIND_URL and PLSREMIND_TOKEN");
  process.exit(1);
}
const once = process.env.PLSREMIND_ONCE === "1";

const client = new Client({ url, token, intents: ["deliveries"] });

// The hub sends an unconfirmed reminder again after every reconnect.
const reminded = new Set<string>();

function describe(delivery: Delivery): string {
  const { reminder, item, user } = delivery;
  let what: string;
  if (reminder.kind === "before" && reminder.offset !== undefined) {
    what = `is due in ${formatDuration(reminder.offset)}`;
  } else if (reminder.kind === "due") {
    what = "is due now";
  } else if (reminder.kind === "follow_up") {
    what = "is overdue";
  } else {
    what = "you asked to be reminded";
  }
  const meant = reminder.scheduledFor.toLocaleTimeString([], {
    hour: "2-digit",
    minute: "2-digit",
  });
  const late = delivery.late ? ` (was meant for ${meant})` : "";
  return `${user.displayName}: "${item.title}" ${what}${late}`;
}

client.on("delivery.created", async (event) => {
  const delivery = event.data;
  if (!reminded.has(delivery.id)) {
    reminded.add(delivery.id);
    console.log(describe(delivery));
  }
  try {
    await client.rest.acknowledgeDelivery(delivery.id, { status: "delivered" });
  } catch (error) {
    // The hub withdrew the reminder in the meantime, for example because the item was completed.
    if (!(error instanceof ApiError && error.code === "delivery_closed")) throw error;
  }
  if (once) {
    client.close();
    process.exit(0);
  }
});

client.on("delivery.closed", (event) => {
  console.log(`a reminder was withdrawn: ${event.data.id} (${event.data.status})`);
});
client.on("disconnected", (event) => {
  console.log(`connection lost (${event.code ?? "no close code"}), reconnecting`);
});
client.on("closed", (event) => {
  console.error(`the connection ended: ${event.reason}`);
  process.exit(1);
});
client.on("error", (event) => {
  console.error("could not confirm a reminder:", event.error);
});

await client.connect();
console.log("connected, waiting for reminders");
