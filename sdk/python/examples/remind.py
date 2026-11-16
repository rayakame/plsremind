"""A minimal reminder adapter: it prints every reminder it is given and acknowledges it.

Run it with the address of a hub and a personal token that has reminders switched on:

    PLSREMIND_URL=https://remind.example.com PLSREMIND_TOKEN=plsr_pat_... python examples/remind.py
"""

from __future__ import annotations

import os
import sys

import plsremind

url = os.environ.get("PLSREMIND_URL")
token = os.environ.get("PLSREMIND_TOKEN")
if not url or not token:
    sys.exit("set PLSREMIND_URL and PLSREMIND_TOKEN")
once = os.environ.get("PLSREMIND_ONCE") == "1"

client = plsremind.Client(url, token=token, intents=plsremind.Intents.DELIVERIES)

# The hub sends an unacknowledged reminder again after every reconnect.
reminded: set[plsremind.ID] = set()


def describe(delivery: plsremind.Delivery) -> str:
    reminder, item, user = delivery.reminder, delivery.item, delivery.user
    if reminder.kind is plsremind.ReminderKind.BEFORE and reminder.offset is not None:
        what = f"is due in {plsremind.format_duration(reminder.offset)}"
    elif reminder.kind is plsremind.ReminderKind.DUE:
        what = "is due now"
    elif reminder.kind is plsremind.ReminderKind.FOLLOW_UP:
        what = "is overdue"
    else:
        what = "you asked to be reminded"
    meant = reminder.scheduled_for.astimezone(user.timezone).strftime("%H:%M")
    late = f" (was meant for {meant})" if delivery.late else ""
    return f'{user.display_name}: "{item.title}" {what}{late}'


@client.listen()
async def on_delivery(event: plsremind.DeliveryCreateEvent) -> None:
    delivery = event.delivery
    if delivery.id not in reminded:
        reminded.add(delivery.id)
        print(describe(delivery), flush=True)
    try:
        await client.rest.acknowledge_delivery(delivery.id, status=plsremind.DeliveryStatus.DELIVERED)
    except plsremind.ConflictError as error:
        # The hub withdrew the reminder in the meantime, for example because the item was completed.
        if error.code != "delivery_closed":
            raise
    if once:
        await client.close()


@client.listen()
async def on_withdrawn(event: plsremind.DeliveryCloseEvent) -> None:
    print(f"a reminder was withdrawn: {event.delivery_id} ({event.status.value})", flush=True)


@client.listen()
async def on_disconnected(event: plsremind.DisconnectedEvent) -> None:
    print(f"connection lost ({event.code or 'no close code'}), reconnecting", flush=True)


@client.listen()
async def on_ready(event: plsremind.ReadyEvent) -> None:
    del event
    print("connected, waiting for reminders", flush=True)


if __name__ == "__main__":
    try:
        client.run()
    except plsremind.GatewayError as error:
        sys.exit(f"the connection ended: {error}")
