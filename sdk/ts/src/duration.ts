const units = [
  ["d", 86_400_000],
  ["h", 3_600_000],
  ["m", 60_000],
] as const;

const pattern = /^(?:(\d+)d)?(?:(\d+)h)?(?:(\d+)m)?$/;

/** Reads a duration as the API writes it ("3d", "1d12h", "90m") into milliseconds. */
export function parseDuration(text: string): number {
  const match = pattern.exec(text);
  if (!match || text === "") {
    throw new RangeError(`invalid duration ${JSON.stringify(text)}`);
  }
  let total = 0;
  units.forEach(([, size], i) => {
    const part = match[i + 1];
    if (part !== undefined) total += Number(part) * size;
  });
  return total;
}

/** Writes milliseconds the way the API reads durations. Only whole minutes above zero. */
export function formatDuration(ms: number): string {
  if (!Number.isFinite(ms) || ms <= 0 || ms % 60_000 !== 0) {
    throw new RangeError(`a duration must be a positive number of whole minutes, got ${ms}`);
  }
  let rest = ms;
  let text = "";
  for (const [suffix, size] of units) {
    const count = Math.floor(rest / size);
    if (count > 0) text += `${count}${suffix}`;
    rest -= count * size;
  }
  return text;
}
