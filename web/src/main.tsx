import React from "react";
import ReactDOM from "react-dom/client";
import { createHashRouter, RouterProvider } from "react-router-dom";
import "./index.css";
import Layout from "./pages/Layout";
import LibraryHome from "./pages/LibraryHome";
import LibraryDetail from "./pages/LibraryDetail";
import DocumentDetail from "./pages/DocumentDetail";
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
