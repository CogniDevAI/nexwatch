import { useEffect } from "react";
import { BrowserRouter, Routes, Route } from "react-router-dom";
import { AppShell } from "./components/layout/AppShell";
import { ProtectedRoute } from "./components/auth/ProtectedRoute";
import { ToastProvider } from "./components/ui/Toast";
import { Login } from "./pages/Login";
import { StatusPage } from "./pages/StatusPage";
import { Dashboard } from "./pages/Dashboard";
import { ServerDetail } from "./pages/ServerDetail";
import { Alerts } from "./pages/Alerts";
import { AlertHistory } from "./pages/AlertHistory";
import { Silences } from "./pages/Silences";
import { Checks } from "./pages/Checks";
import { Logs } from "./pages/Logs";
import { NotificationChannels } from "./pages/NotificationChannels";
import { Settings } from "./pages/Settings";
import { Users } from "./pages/settings/Users";
import { AuditLog } from "./pages/settings/AuditLog";
import { Agents } from "./pages/Agents";
import { useAuthStore } from "./stores/authStore";

function App() {
  const restoreAuth = useAuthStore((s) => s.restoreAuth);

  useEffect(() => {
    restoreAuth();
  }, [restoreAuth]);

  return (
    <ToastProvider>
      <BrowserRouter>
        <Routes>
          <Route path="/status" element={<StatusPage />} />
          <Route path="/login" element={<Login />} />
          <Route element={<ProtectedRoute />}>
            <Route element={<AppShell />}>
              <Route path="/" element={<Dashboard />} />
              <Route path="/servers/:id" element={<ServerDetail />} />
              <Route path="/alerts" element={<Alerts />} />
              <Route path="/alerts/history" element={<AlertHistory />} />
              <Route path="/alerts/silences" element={<Silences />} />
              <Route path="/agents" element={<Agents />} />
              <Route path="/checks" element={<Checks />} />
              <Route path="/logs" element={<Logs />} />
              <Route path="/settings" element={<Settings />} />
              <Route path="/settings/notifications" element={<NotificationChannels />} />
              <Route path="/settings/users" element={<Users />} />
              <Route path="/settings/audit" element={<AuditLog />} />
            </Route>
          </Route>
        </Routes>
      </BrowserRouter>
    </ToastProvider>
  );
}

export default App;
