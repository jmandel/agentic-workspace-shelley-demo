import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { Router, Route, Switch } from "wouter";
import { HomePage } from "@/pages/HomePage";
import { WorkspacePage } from "@/pages/WorkspacePage";
import { TopicPage } from "@/pages/TopicPage";
import { WorkspaceFilesPage } from "@/pages/WorkspaceFilesPage";
import { AboutPage } from "@/pages/AboutPage";
import "@/index.css";

function App() {
  return (
    <Router>
      <Switch>
        <Route path="/" component={HomePage} />
        <Route
          path="/app/:namespace/:workspace/:topic/files"
          component={WorkspaceFilesPage}
        />
        <Route
          path="/app/:namespace/:workspace/:topic"
          component={TopicPage}
        />
        <Route
          path="/app/:namespace/:workspace"
          component={WorkspacePage}
        />
        <Route path="/about" component={AboutPage} />
        <Route>
          <div className="page">
            <div className="card">
              <h1>Not Found</h1>
              <p>
                <a href="/" className="btn btn-primary">
                  Back to Manager
                </a>
              </p>
            </div>
          </div>
        </Route>
      </Switch>
    </Router>
  );
}

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
