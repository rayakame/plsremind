import "./style/system.css";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";

createRoot(document.getElementById("app")!).render(
  <StrictMode>
    <h1 className="h1">plsremind</h1>
  </StrictMode>,
);
