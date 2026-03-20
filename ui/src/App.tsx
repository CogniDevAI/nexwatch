import { useEffect } from "react";
import { BrowserRouter, Routes, Route } from "react-router-dom";
import { AppShell } from "./components/layout/AppShell";
import { ProtectedRoute } from "./components/auth/ProtectedRoute";
import { Login } from "./pages/Login";
import { Dashboard } from "./pages/Dashboard";
import { ServerDetail } from "./pages/ServerDetail";
import { Alerts } from "./pages/Alerts";
import { AlertHistory } from "./pages/AlertHistory";
import { NotificationChannels } from "./pages/NotificationChannels";
import { Settings } from "./pages/Settings";
import { Agents } from "./pages/Agents";
import { useAuthStore } from "./stores/authStore";

function App() {
  const restoreAuth = useAuthStore((s) => s.restoreAuth);

  useEffect(() => {
    restoreAuth();
  }, [restoreAuth]);

  return (
    <BrowserRouter>
      <Routes>
        <Route path="/login" element={<Login />} />
        <Route element={<ProtectedRoute />}>
          <Route element={<AppShell />}>
            <Route path="/" element={<Dashboard />} />
            <Route path="/servers/:id" element={<ServerDetail />} />
            <Route path="/alerts" element={<Alerts />} />
            <Route path="/alerts/history" element={<AlertHistory />} />
            <Route path="/agents" element={<Agents />} />
            <Route path="/settings" element={<Settings />} />
            <Route path="/settings/notifications" element={<NotificationChannels />} />
          </Route>
        </Route>
      </Routes>
    </BrowserRouter>
  );
}

export default App;
