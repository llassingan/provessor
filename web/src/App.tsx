import { Routes, Route, Navigate } from "react-router-dom";
import { useState, useEffect, type ReactNode } from "react";
import Layout from "./components/Layout";
import Login from "./pages/Login";
import Signup from "./pages/Signup";
import Dashboard from "./pages/Dashboard";
import NewVPS from "./pages/NewVPS";
import VPSDetail from "./pages/VPSDetail";
import SettingsPage from "./pages/Settings";
import CustomTemplate from "./pages/CustomTemplate";
import { settings } from "./lib/api";
import type { Settings } from "./lib/api";

function ProtectedRoute({ children }: { children: ReactNode }) {
  const [checking, setChecking] = useState(true);
  const [authorized, setAuthorized] = useState(false);

  useEffect(() => {
    let cancelled = false;
    settings
      .get()
      .then(() => {
        if (!cancelled) {
          setAuthorized(true);
          setChecking(false);
        }
      })
      .catch(() => {
        if (!cancelled) {
          setAuthorized(false);
          setChecking(false);
        }
      });
    return () => {
      cancelled = true;
    };
  }, []);

  if (checking) {
    return (
      <div className="flex h-screen items-center justify-center">
        <div className="h-8 w-8 animate-spin rounded-full border-4 border-primary-200 border-t-primary-600" />
      </div>
    );
  }

  if (!authorized) {
    return <Navigate to="/login" replace />;
  }

  return <>{children}</>;
}

export default function App(): JSX.Element {
  const [appSettings, setAppSettings] = useState<Settings | null>(null);

  const fetchSettings = (): void => {
    settings
      .get()
      .then(setAppSettings)
      .catch(() => {
        setAppSettings(null);
      });
  };

  useEffect(() => {
    fetchSettings();
  }, []);

  return (
    <Routes>
      <Route path="/" element={<Navigate to="/dashboard" replace />} />
      <Route path="/login" element={<Login />} />
      <Route path="/signup" element={<Signup />} />
      <Route
        path="/dashboard"
        element={
          <ProtectedRoute>
            <Layout settings={appSettings} onSettingsRefresh={fetchSettings}>
              <Dashboard
                settings={appSettings}
                onSettingsRefresh={fetchSettings}
              />
            </Layout>
          </ProtectedRoute>
        }
      />
      <Route
        path="/vps/new"
        element={
          <ProtectedRoute>
            <Layout settings={appSettings} onSettingsRefresh={fetchSettings}>
              <NewVPS />
            </Layout>
          </ProtectedRoute>
        }
      />
      <Route
        path="/vps/:id"
        element={
          <ProtectedRoute>
            <Layout settings={appSettings} onSettingsRefresh={fetchSettings}>
              <VPSDetail />
            </Layout>
          </ProtectedRoute>
        }
      />
      <Route
        path="/templates/new"
        element={
          <ProtectedRoute>
            <Layout settings={appSettings} onSettingsRefresh={fetchSettings}>
              <CustomTemplate />
            </Layout>
          </ProtectedRoute>
        }
      />
      <Route
        path="/settings"
        element={
          <ProtectedRoute>
            <Layout settings={appSettings} onSettingsRefresh={fetchSettings}>
              <SettingsPage
                settings={appSettings}
                onSettingsRefresh={fetchSettings}
              />
            </Layout>
          </ProtectedRoute>
        }
      />
    </Routes>
  );
}
