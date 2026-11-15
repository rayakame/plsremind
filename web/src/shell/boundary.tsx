import { Component, type ReactNode } from "react";
import { t } from "../i18n/index.js";

/** Catches a page that cannot be drawn, such as one whose script is gone after an update. */
export class Boundary extends Component<{ children: ReactNode }, { failed: boolean }> {
  override state = { failed: false };

  static getDerivedStateFromError(): { failed: boolean } {
    return { failed: true };
  }

  override render(): ReactNode {
    if (!this.state.failed) return this.props.children;
    return (
      <main className="center">
        <h1 className="h1">{t("errors.unexpected")}</h1>
        <p className="lead">{t("errors.reloadText")}</p>
        <div className="line">
          <button type="button" className="btn primary" onClick={() => location.reload()}>
            {t("errors.reload")}
          </button>
        </div>
      </main>
    );
  }
}
