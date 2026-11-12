import React from "react";
import ReactDOM from "react-dom/client";
import { createHashRouter, RouterProvider } from "react-router-dom";
import "./index.css";
import { initTheme } from "./theme";

initTheme();
import Layout from "./pages/Layout";
import LibraryHome from "./pages/LibraryHome";
import LibraryDetail from "./pages/LibraryDetail";
import DocumentDetail from "./pages/DocumentDetail";
import Cards from "./pages/Cards";
import Quiz from "./pages/Quiz";
import AgentReview from "./pages/AgentReview";
import Review from "./pages/Review";
import Settings from "./pages/Settings";

// hash 路由：配合 go:embed 单二进制部署（无需 SPA fallback 服务端配置）。
const router = createHashRouter([
  {
    path: "/",
    element: <Layout />,
    children: [
      { index: true, element: <LibraryHome /> },
      { path: "library/:libId", element: <LibraryDetail /> },
      { path: "library/:libId/cards", element: <Cards /> },
      { path: "library/:libId/quiz", element: <Quiz /> },
      { path: "library/:libId/agent", element: <AgentReview /> },
      { path: "document/:docId", element: <DocumentDetail /> },
      { path: "review/:libId", element: <Review /> },
      { path: "settings", element: <Settings /> },
    ],
  },
]);

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <RouterProvider router={router} />
  </React.StrictMode>
);
