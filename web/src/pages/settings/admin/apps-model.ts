const discordAuthorize = "https://discord.com/oauth2/authorize";

/** A Discord Application ID: a snowflake of 17 to 20 digits. */
export function isApplicationId(text: string): boolean {
  return /^\d{17,20}$/.test(text);
}

/** The link that adds a Discord application to a user's account. */
export function discordInstallUrl(applicationId: string): string {
  return `${discordAuthorize}?client_id=${applicationId}&integration_type=1&scope=applications.commands`;
}

/** The Application ID of a link made by discordInstallUrl, and nothing for any other link. */
export function discordApplicationIdOf(url: string): string | undefined {
  try {
    const parsed = new URL(url);
    const id = parsed.searchParams.get("client_id") ?? "";
    return parsed.href === new URL(discordInstallUrl(id)).href && isApplicationId(id)
      ? id
      : undefined;
  } catch {
    return undefined;
  }
}

/** Empty, or an https address the hub takes as a link to set an app up. */
export function isInstallUrl(text: string): boolean {
  if (text === "") return true;
  try {
    const url = new URL(text);
    return url.protocol === "https:" && url.host !== "" && text.length <= 512;
  } catch {
    return false;
  }
}
