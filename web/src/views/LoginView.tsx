import React, { useState } from 'react';
import { api } from '../lib/api';
import { Button } from '../components/ui/Button';
import { Input } from '../components/ui/Input';
import { Card, CardHeader, CardTitle, CardDescription, CardContent, CardFooter } from '../components/ui/Card';
import { useToast } from '../components/ui/Toast';
import { Zap, Lock, Mail, ShieldCheck } from 'lucide-react';
import { User } from '../lib/types';

export interface LoginViewProps {
  onLoginSuccess: (user: User) => void;
}

export function LoginView({ onLoginSuccess }: LoginViewProps) {
  const [email, setEmail] = useState('admin@pikpik.local');
  const [password, setPassword] = useState('');
  const [isLoading, setIsLoading] = useState(false);
  const toast = useToast();

  const handleLogin = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!email || !password) {
      toast.error('Validation Error', 'Please provide both email and password');
      return;
    }

    setIsLoading(true);
    try {
      const resp = await api.auth.login({ email, password });
      toast.success('Authenticated', `Welcome back, ${resp.user.email}`);
      onLoginSuccess(resp.user);
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : 'Invalid credentials';
      toast.error('Authentication Failed', msg);
    } finally {
      setIsLoading(false);
    }
  };

  const handlePasskeyLogin = async () => {
    setIsLoading(true);
    try {
      await api.auth.passkeyBegin();
      const finish = await api.auth.passkeyFinish({ signature: 'mock_passkey_sig' });
      if (finish.token) {
        const user = await api.auth.me();
        toast.success('Authenticated with Passkey', `Welcome back, ${user.email}`);
        onLoginSuccess(user);
      }
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : 'Passkey login failed';
      toast.error('Passkey Error', msg);
    } finally {
      setIsLoading(false);
    }
  };

  return (
    <div className="min-h-screen bg-[#09090b] flex flex-col justify-center items-center p-4">
      {/* Glow background accent */}
      <div className="absolute top-1/3 left-1/2 -translate-x-1/2 -translate-y-1/2 w-96 h-96 bg-cyan-500/10 rounded-full blur-3xl pointer-events-none" />

      <div className="w-full max-w-md relative z-10">
        <div className="flex flex-col items-center text-center mb-8">
          <div className="h-12 w-12 rounded-xl bg-gradient-to-tr from-cyan-500 to-cyan-400 flex items-center justify-center shadow-lg shadow-cyan-500/20 mb-3">
            <Zap className="h-6 w-6 text-zinc-950 fill-zinc-950" />
          </div>
          <h1 className="text-2xl font-bold tracking-tight text-zinc-100 font-mono">
            pikpik control plane
          </h1>
          <p className="text-xs text-zinc-400 mt-1">
            Sub-millisecond Swarm orchestration & ingress runtime
          </p>
        </div>

        <Card className="border-zinc-800/80 bg-zinc-900/80 shadow-2xl">
          <form onSubmit={handleLogin}>
            <CardHeader>
              <CardTitle>Authentication</CardTitle>
              <CardDescription>
                Sign in to manage your cluster, applications, and routing.
              </CardDescription>
            </CardHeader>

            <CardContent className="space-y-4">
              <Input
                label="Email"
                type="email"
                placeholder="admin@pikpik.local"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                leftIcon={<Mail className="h-4 w-4" />}
                required
                autoComplete="username"
              />

              <Input
                label="Password"
                type="password"
                placeholder="••••••••••••"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                leftIcon={<Lock className="h-4 w-4" />}
                required
                autoComplete="current-password"
              />
            </CardContent>

            <CardFooter className="flex flex-col gap-3">
              <Button
                type="submit"
                variant="primary"
                className="w-full"
                isLoading={isLoading}
              >
                Sign In to Cluster
              </Button>

              <div className="relative w-full my-1">
                <div className="absolute inset-0 flex items-center">
                  <div className="w-full border-t border-zinc-800" />
                </div>
                <div className="relative flex justify-center text-[10px] uppercase">
                  <span className="bg-zinc-900 px-2 text-zinc-500 font-medium">Or</span>
                </div>
              </div>

              <Button
                type="button"
                variant="secondary"
                className="w-full gap-2"
                onClick={handlePasskeyLogin}
                disabled={isLoading}
              >
                <ShieldCheck className="h-4 w-4 text-cyan-400" />
                <span>Sign in with Passkey / WebAuthn</span>
              </Button>
            </CardFooter>
          </form>
        </Card>

        <p className="text-center text-xs text-zinc-600 mt-6">
          Pikpik Unified Runtime v0.2 • Zero External Dependencies
        </p>
      </div>
    </div>
  );
}
