import { Routes, Route, Navigate } from "react-router-dom";
import { useState, useEffect, type ReactNode } from "react";
import Layout from "./components/Layout";
import Login from "./pages/Login";
import Signup from "./pages/Signup";
import Dashboard from "./pages/Dashboard";
import NewVPS from "./pages/NewVPS";
import VPSDetail from "./pages/VPSDetail";
import VPSManage from "./pages/VPSManage";
import SettingsPage from "./pages/Settings";
import CustomTemplate from "./pages/CustomTemplate";
import Networks from "./pages/Networks";
import NewNetwork from "./pages/NewNetwork";
import { settings } from "./lib/api";
import type { Settings } from "./lib/api";

const API_BASE = (import.meta.env.VITE_API_BASE_URL as string | undefined) ?? "/api";

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
      .catch((err: unknown) => {
        if (!cancelled) {
          if (err instanceof Error && err.message === "Unauthorized") {
            setAuthorized(false);
          } else {
            setAuthorized(true);
          }
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

function InitGuard({ children }: { children: ReactNode }) {
  const [phase, setPhase] = useState<"checking" | "has_users" | "no_users">("checking");

  useEffect(() => {
    let cancelled = false;
    fetch(`${API_BASE}/auth/init`, { credentials: "include" })
      .then((r) => r.json())
      .then((data: { has_users: boolean }) => {
        if (!cancelled) setPhase(data.has_users ? "has_users" : "no_users");
      })
      .catch(() => {
        if (!cancelled) setPhase("has_users");
      });
    return () => { cancelled = true; };
  }, []);

  if (phase === "checking") {
    return (
      <div className="flex h-screen items-center justify-center">
        <div className="h-8 w-8 animate-spin rounded-full border-4 border-primary-200 border-t-primary-600" />
      </div>
    );
  }

  if (phase === "no_users") {
    return <Navigate to="/signup" replace />;
  }

  return <>{children}</>;
}

function LoginGuard({ children }: { children: ReactNode }) {
  const [phase, setPhase] = useState<"checking" | "has_users" | "no_users">("checking");

  useEffect(() => {
    let cancelled = false;
    fetch(`${API_BASE}/auth/init`, { credentials: "include" })
      .then((r) => r.json())
      .then((data: { has_users: boolean }) => {
        if (!cancelled) setPhase(data.has_users ? "has_users" : "no_users");
      })
      .catch(() => {
        if (!cancelled) setPhase("has_users");
      });
    return () => { cancelled = true; };
  }, []);

  if (phase === "checking") {
    return (
      <div className="flex h-screen items-center justify-center">
        <div className="h-8 w-8 animate-spin rounded-full border-4 border-primary-200 border-t-primary-600" />
      </div>
    );
  }

  if (phase === "no_users") {
    return <Navigate to="/signup" replace />;
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
      <Route path="/" element={<InitGuard><Navigate to="/dashboard" replace /></InitGuard>} />
      <Route path="/login" element={<LoginGuard><Login /></LoginGuard>} />
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
        path="/networks"
        element={
          <ProtectedRoute>
            <Layout settings={appSettings} onSettingsRefresh={fetchSettings}>
              <Networks />
            </Layout>
          </ProtectedRoute>
        }
      />
      <Route
        path="/networks/new"
        element={
          <ProtectedRoute>
            <Layout settings={appSettings} onSettingsRefresh={fetchSettings}>
              <NewNetwork />
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
        path="/vps/:id/manage"
        element={
          <ProtectedRoute>
            <Layout settings={appSettings} onSettingsRefresh={fetchSettings}>
              <VPSManage />
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
