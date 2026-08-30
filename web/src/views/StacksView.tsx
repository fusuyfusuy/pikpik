import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '../lib/api';
import { Stack, CreateStackRequest } from '../lib/types';
import { Card } from '../components/ui/Card';
import { Button } from '../components/ui/Button';
import { Badge } from '../components/ui/Badge';
import { Input, Textarea } from '../components/ui/Input';
import { Modal } from '../components/ui/Modal';
import { useToast } from '../components/ui/Toast';
import { formatDate } from '../lib/utils';
import { Layers, Plus, Play, Trash2, Code2 } from 'lucide-react';

const SAMPLE_COMPOSE = `version: '3.8'
services:
  web:
    image: nginx:alpine
    ports:
      - "80:80"
    deploy:
      replicas: 2
      restart_policy:
        condition: on-failure
  redis:
    image: redis:7-alpine
    deploy:
      replicas: 1
`;

export function StacksView() {
  const queryClient = useQueryClient();
  const toast = useToast();

  const [isCreateModalOpen, setIsCreateModalOpen] = useState(false);
  const [selectedStack, setSelectedStack] = useState<Stack | null>(null);
  const [stackName, setStackName] = useState('');
  const [composeYAML, setComposeYAML] = useState(SAMPLE_COMPOSE);

  const { data: stacks, isLoading } = useQuery({
    queryKey: ['stacks'],
    queryFn: api.stacks.list,
  });

  const createMutation = useMutation({
    mutationFn: (req: CreateStackRequest) => api.stacks.create(req),
    onSuccess: (newStack) => {
      queryClient.invalidateQueries({ queryKey: ['stacks'] });
      toast.success('Stack Created', `Stack ${newStack.name} created successfully`);
      setIsCreateModalOpen(false);
      setStackName('');
      setComposeYAML(SAMPLE_COMPOSE);
    },
    onError: (err: Error) => toast.error('Create Stack Failed', err.message),
  });

  const deployMutation = useMutation({
    mutationFn: (id: string) => api.stacks.deploy(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['stacks'] });
      toast.success('Stack Deployed', 'Swarm compose stack deploy in progress');
    },
    onError: (err: Error) => toast.error('Deploy Failed', err.message),
  });

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.stacks.delete(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['stacks'] });
      toast.success('Stack Deleted', 'Stack services removed');
      if (selectedStack) setSelectedStack(null);
    },
    onError: (err: Error) => toast.error('Delete Failed', err.message),
  });

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
        <div>
          <h1 className="text-xl font-bold tracking-tight text-zinc-100 flex items-center gap-2">
            <Layers className="h-5 w-5 text-cyan-400" />
            <span>Compose Stacks</span>
          </h1>
          <p className="text-xs text-zinc-400 mt-0.5">
            Multi-service Docker Compose declarations deployed natively on Swarm
          </p>
        </div>

        <Button
          variant="primary"
          size="sm"
          onClick={() => setIsCreateModalOpen(true)}
          leftIcon={<Plus className="h-4 w-4" />}
        >
          Create Stack
        </Button>
      </div>

      {/* Grid of Stacks */}
      {isLoading ? (
        <div className="text-center py-12 text-zinc-500 text-xs">Loading stacks...</div>
      ) : (!stacks || stacks.length === 0) ? (
        <Card className="text-center py-12">
          <Layers className="h-8 w-8 text-zinc-600 mx-auto mb-2" />
          <h3 className="text-sm font-semibold text-zinc-200">No Compose Stacks Found</h3>
          <p className="text-xs text-zinc-500 mt-1 max-w-sm mx-auto">
            Define multi-container topologies using standard docker-compose.yml files.
          </p>
          <div className="mt-4">
            <Button variant="primary" size="sm" onClick={() => setIsCreateModalOpen(true)}>
              Deploy First Stack
            </Button>
          </div>
        </Card>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          {stacks.map((stack) => (
            <Card
              key={stack.id}
              className="flex flex-col justify-between hover:border-zinc-700 cursor-pointer"
              onClick={() => setSelectedStack(stack)}
            >
              <div>
                <div className="flex items-start justify-between">
                  <div className="flex items-center gap-2">
                    <div className="p-2 rounded-lg bg-zinc-800 text-cyan-400">
                      <Layers className="h-4 w-4" />
                    </div>
                    <div>
                      <h3 className="text-sm font-bold text-zinc-100">{stack.name}</h3>
                      <span className="text-[11px] text-zinc-500 font-mono">
                        {formatDate(stack.created_at)}
                      </span>
                    </div>
                  </div>
                  <Badge variant={stack.status === 'active' ? 'success' : 'default'} dot>
                    {stack.status}
                  </Badge>
                </div>

                <div className="mt-4">
                  <span className="text-xs text-zinc-500 font-medium">Services:</span>
                  <div className="flex flex-wrap gap-1.5 mt-1.5">
                    {stack.services && stack.services.length > 0 ? (
                      stack.services.map((s) => (
                        <span
                          key={s}
                          className="px-2 py-0.5 rounded bg-zinc-950 border border-zinc-800 text-zinc-300 font-mono text-[11px]"
                        >
                          {s}
                        </span>
                      ))
                    ) : (
                      <span className="text-xs text-zinc-600 font-mono">swarm_stack</span>
                    )}
                  </div>
                </div>
              </div>

              <div
                className="flex items-center justify-end gap-2 pt-4 mt-4 border-t border-zinc-800/60"
                onClick={(e) => e.stopPropagation()}
              >
                <Button
                  variant="subtle"
                  size="sm"
                  onClick={() => deployMutation.mutate(stack.id)}
                  isLoading={deployMutation.isPending && deployMutation.variables === stack.id}
                  leftIcon={<Play className="h-3 w-3" />}
                >
                  Deploy
                </Button>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => {
                    if (confirm(`Delete stack ${stack.name}?`)) {
                      deleteMutation.mutate(stack.id);
                    }
                  }}
                >
                  <Trash2 className="h-3.5 w-3.5 text-rose-400" />
                </Button>
              </div>
            </Card>
          ))}
        </div>
      )}

      {/* Inspect Stack YAML Modal */}
      {selectedStack && (
        <Modal
          isOpen={Boolean(selectedStack)}
          onClose={() => setSelectedStack(null)}
          title={
            <div className="flex items-center gap-2">
              <Code2 className="h-5 w-5 text-cyan-400" />
              <span>{selectedStack.name} (docker-compose.yml)</span>
            </div>
          }
          size="lg"
        >
          <div className="space-y-4">
            <pre className="p-4 rounded-lg bg-zinc-950 border border-zinc-800 font-mono text-xs text-zinc-300 leading-relaxed overflow-x-auto max-h-96">
              {selectedStack.compose_yaml}
            </pre>

            <div className="flex justify-end gap-2 pt-3 border-t border-zinc-800">
              <Button
                variant="primary"
                size="sm"
                onClick={() => {
                  deployMutation.mutate(selectedStack.id);
                  setSelectedStack(null);
                }}
              >
                Trigger Stack Rollout
              </Button>
            </div>
          </div>
        </Modal>
      )}

      {/* Create Stack Modal */}
      <Modal
        isOpen={isCreateModalOpen}
        onClose={() => setIsCreateModalOpen(false)}
        title="Create Docker Compose Stack"
        description="Deploy multi-service YAML directly to Swarm"
        size="lg"
      >
        <form
          onSubmit={(e) => {
            e.preventDefault();
            createMutation.mutate({ name: stackName, compose_yaml: composeYAML });
          }}
          className="space-y-4"
        >
          <Input
            label="Stack Name"
            placeholder="analytics-stack"
            value={stackName}
            onChange={(e) => setStackName(e.target.value)}
            required
          />

          <Textarea
            label="docker-compose.yml"
            rows={12}
            value={composeYAML}
            onChange={(e) => setComposeYAML(e.target.value)}
            required
          />

          <div className="flex justify-end gap-2 pt-4 border-t border-zinc-800">
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => setIsCreateModalOpen(false)}
            >
              Cancel
            </Button>
            <Button
              type="submit"
              variant="primary"
              size="sm"
              isLoading={createMutation.isPending}
            >
              Save & Deploy Stack
            </Button>
          </div>
        </form>
      </Modal>
    </div>
  );
}
