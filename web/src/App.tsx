import { useState, useEffect, lazy, Suspense } from 'react';
import { useQuery } from '@tanstack/react-query';
import { api, getToken } from './lib/api';
import { Layout } from './components/Layout';
import { useSSE } from './hooks/useSSE';
import { User } from './lib/types';
import { ErrorBoundary } from './components/ErrorBoundary';

// Lazy-loaded Views for route-level code splitting
const LoginView = lazy(() => import('./views/LoginView').then((m) => ({ default: m.LoginView })));
const DashboardView = lazy(() => import('./views/DashboardView').then((m) => ({ default: m.DashboardView })));
const ProjectsView = lazy(() => import('./views/ProjectsView').then((m) => ({ default: m.ProjectsView })));
const MarketplaceView = lazy(() => import('./views/MarketplaceView').then((m) => ({ default: m.MarketplaceView })));
const AppsView = lazy(() => import('./views/AppsView').then((m) => ({ default: m.AppsView })));
const StacksView = lazy(() => import('./views/StacksView').then((m) => ({ default: m.StacksView })));
const NodesView = lazy(() => import('./views/NodesView').then((m) => ({ default: m.NodesView })));
const DatabasesView = lazy(() => import('./views/DatabasesView').then((m) => ({ default: m.DatabasesView })));
const IngressView = lazy(() => import('./views/IngressView').then((m) => ({ default: m.IngressView })));
const BackupsView = lazy(() => import('./views/BackupsView').then((m) => ({ default: m.BackupsView })));
const RegistryView = lazy(() => import('./views/RegistryView').then((m) => ({ default: m.RegistryView })));
const SystemView = lazy(() => import('./views/SystemView').then((m) => ({ default: m.SystemView })));
const IntegrationsView = lazy(() => import('./views/IntegrationsView').then((m) => ({ default: m.IntegrationsView })));
const SettingsView = lazy(() => import('./views/SettingsView').then((m) => ({ default: m.SettingsView })));

function ViewLoader() {
  return (
    <div className="flex items-center justify-center h-64 text-zinc-500">
      <div className="flex flex-col items-center gap-2">
        <div className="h-5 w-5 border-2 border-cyan-500 border-t-transparent rounded-full animate-spin" />
        <span className="text-xs font-mono text-zinc-400">Loading module...</span>
      </div>
    </div>
  );
}

export function App() {
  const [currentUser, setCurrentUser] = useState<User | null>(null);
  const [currentView, setCurrentView] = useState<string>(() => {
    const hash = window.location.hash.replace('#/', '').replace('#', '');
    return hash || 'dashboard';
  });

  // Verify auth session on load
  const { data: user, isLoading: isAuthLoading, refetch: refetchUser } = useQuery({
    queryKey: ['currentUser'],
    queryFn: api.auth.me,
    enabled: Boolean(getToken()),
    retry: false,
  });

  useEffect(() => {
    if (user) {
      setCurrentUser(user);
    }
  }, [user]);

  // URL Hash Sync for routing
  useEffect(() => {
    const handleHashChange = () => {
      const hash = window.location.hash.replace('#/', '').replace('#', '');
      if (hash) setCurrentView(hash);
    };

    window.addEventListener('hashchange', handleHashChange);
    return () => window.removeEventListener('hashchange', handleHashChange);
  }, []);

  const handleNavigate = (view: string) => {
    setCurrentView(view);
    window.location.hash = `/${view}`;
  };

  // Real-time Event Stream
  const { status: sseStatus } = useSSE({
    endpoint: '/api/v1/events/stream',
    enabled: Boolean(currentUser),
  });

  const handleLoginSuccess = (loggedInUser: User) => {
    setCurrentUser(loggedInUser);
    refetchUser();
  };

  const handleLogout = async () => {
    await api.auth.logout();
    setCurrentUser(null);
  };

  if (isAuthLoading) {
    return (
      <div className="min-h-screen bg-[#09090b] flex flex-col items-center justify-center text-zinc-400">
        <div className="h-6 w-6 border-2 border-cyan-500 border-t-transparent rounded-full animate-spin mb-3" />
        <span className="text-xs font-mono">Connecting to pikpik control plane...</span>
      </div>
    );
  }

  if (!currentUser) {
    return (
      <Suspense
        fallback={
          <div className="min-h-screen bg-[#09090b] flex flex-col items-center justify-center text-zinc-400">
            <div className="h-6 w-6 border-2 border-cyan-500 border-t-transparent rounded-full animate-spin mb-3" />
            <span className="text-xs font-mono">Loading authentication...</span>
          </div>
        }
      >
        <LoginView onLoginSuccess={handleLoginSuccess} />
      </Suspense>
    );
  }

  return (
    <Layout
      currentView={currentView}
      onNavigate={handleNavigate}
      user={currentUser}
      onLogout={handleLogout}
      sseStatus={sseStatus}
    >
      <ErrorBoundary key={currentView}>
        <Suspense fallback={<ViewLoader />}>
          {currentView === 'dashboard' && <DashboardView onNavigate={handleNavigate} />}
          {currentView === 'projects' && <ProjectsView onNavigate={handleNavigate} />}
          {(currentView === 'marketplace' || currentView === 'templates') && <MarketplaceView />}
          {currentView === 'apps' && <AppsView />}
          {currentView === 'stacks' && <StacksView />}
          {currentView === 'nodes' && <NodesView />}
          {currentView === 'databases' && <DatabasesView />}
          {currentView === 'ingress' && <IngressView />}
          {currentView === 'backups' && <BackupsView />}
          {currentView === 'registry' && <RegistryView />}
          {currentView === 'integrations' && <IntegrationsView />}
          {currentView === 'system' && <SystemView />}
          {currentView === 'settings' && <SettingsView user={currentUser} />}
        </Suspense>
      </ErrorBoundary>
    </Layout>
  );
}

export default App;
