import "./style/system.css";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./app.js";

createRoot(document.getElementById("app")!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
