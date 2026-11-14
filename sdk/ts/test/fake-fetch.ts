export interface Call {
  method: string;
  url: string;
  headers: Record<string, string>;
  body: unknown;
  credentials: RequestCredentials | undefined;
}

export interface Answer {
  status: number;
  body?: unknown;
  headers?: Record<string, string>;
  text?: string;
}

/** A fetch that records what it is asked and answers from a list, repeating the last answer. */
export function fakeFetch(answers: (Answer | Error)[]) {
  const calls: Call[] = [];
  let next = 0;
  const fetch = (async (input: RequestInfo | URL, init: RequestInit = {}) => {
    const headers: Record<string, string> = {};
    new Headers(init.headers).forEach((value, key) => (headers[key] = value));
    calls.push({
      method: init.method ?? "GET",
      url: String(input),
      headers,
      body: typeof init.body === "string" ? JSON.parse(init.body) : undefined,
      credentials: init.credentials,
    });
    init.signal?.throwIfAborted();
    const answer = answers[Math.min(next++, answers.length - 1)];
    if (answer === undefined) throw new Error("no answer prepared");
    if (answer instanceof Error) throw answer;
    const text = answer.text ?? (answer.body === undefined ? null : JSON.stringify(answer.body));
    return new Response(answer.status === 204 ? null : text, {
      status: answer.status,
      headers: answer.headers ?? {},
    });
  }) as typeof globalThis.fetch;
  return { fetch, calls };
}
