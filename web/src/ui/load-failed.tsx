import { useT } from "../i18n/index.js";
import { Button } from "./controls.js";

/** Says that something did not load and offers to ask again. */
export function LoadFailed(props: { onRetry: () => void }) {
  const t = useT();
  return (
    <div className="between">
      <span className="dim">{t("common.loadFailed")}</span>
      <Button onClick={props.onRetry}>{t("common.retry")}</Button>
    </div>
  );
}
