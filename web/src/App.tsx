import { useState, useEffect } from 'react';
import { useQuery } from '@tanstack/react-query';
import { api, getToken } from './lib/api';
import { Layout } from './components/Layout';
import { useSSE } from './hooks/useSSE';
import { User } from './lib/types';

// Views
import { LoginView } from './views/LoginView';
import { DashboardView } from './views/DashboardView';
import { AppsView } from './views/AppsView';
import { StacksView } from './views/StacksView';
import { NodesView } from './views/NodesView';
import { DatabasesView } from './views/DatabasesView';
import { IngressView } from './views/IngressView';
import { BackupsView } from './views/BackupsView';
import { RegistryView } from './views/RegistryView';
import { SystemView } from './views/SystemView';
import { SettingsView } from './views/SettingsView';

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
    return <LoginView onLoginSuccess={handleLoginSuccess} />;
  }

  return (
    <Layout
      currentView={currentView}
      onNavigate={handleNavigate}
      user={currentUser}
      onLogout={handleLogout}
      sseStatus={sseStatus}
    >
      {currentView === 'dashboard' && <DashboardView onNavigate={handleNavigate} />}
      {currentView === 'apps' && <AppsView />}
      {currentView === 'stacks' && <StacksView />}
      {currentView === 'nodes' && <NodesView />}
      {currentView === 'databases' && <DatabasesView />}
      {currentView === 'ingress' && <IngressView />}
      {currentView === 'backups' && <BackupsView />}
      {currentView === 'registry' && <RegistryView />}
      {currentView === 'system' && <SystemView />}
      {currentView === 'settings' && <SettingsView user={currentUser} />}
    </Layout>
  );
}

export default App;
