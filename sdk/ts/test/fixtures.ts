type Wire = Record<string, unknown>;

export function wireItem(overrides: Wire = {}): Wire {
  return {
    id: "i1",
    list_id: "l1",
    title: "Hand in the report",
    note: "",
    priority: "normal",
    due_at: null,
    due_date: null,
    all_day: false,
    status: "open",
    completed_at: null,
    reminder_override: null,
    subitems: [],
    reminders: [],
    created_at: "2026-11-01T10:00:00Z",
    updated_at: "2026-11-01T10:00:00Z",
    ...overrides,
  };
}

export function wireList(overrides: Wire = {}): Wire {
  return {
    id: "l1",
    name: "Inbox",
    position: 0,
    is_inbox: true,
    created_at: "2026-11-01T10:00:00Z",
    updated_at: "2026-11-01T10:00:00Z",
    ...overrides,
  };
}

export function wirePlan(overrides: Wire = {}): Wire {
  return { priority: "normal", before: ["1d"], follow_up: "never", is_default: true, ...overrides };
}

export function wireUser(overrides: Wire = {}): Wire {
  return {
    id: "u1",
    display_name: "Mia",
    role: "user",
    timezone: "Europe/Berlin",
    quiet_hours: null,
    locale: "de",
    preferences: {},
    created_at: "2026-11-01T10:00:00Z",
    ...overrides,
  };
}

export function wireDelivery(overrides: Wire = {}): Wire {
  return {
    id: "d1",
    attempt: 1,
    late: false,
    reminder: { id: "r1", kind: "before", offset: "1d", scheduled_for: "2026-11-12T18:00:00Z" },
    item: wireItem({ due_at: "2026-11-13T18:00:00Z" }),
    user: {
      id: "u1",
      display_name: "Mia",
      timezone: "Europe/Berlin",
      locale: "de",
      external_ids: ["1234"],
    },
    ...overrides,
  };
}

export function sync(
  userId: string,
  chunk: { index: number; count: number },
  parts: Wire = {},
): Wire {
  return { user_id: userId, chunk, ...parts };
}

export function change(userId: string, data: Wire, nonce: string | null = null): Wire {
  return { user_id: userId, actor: { kind: "session", id: "c1" }, nonce, data };
}
