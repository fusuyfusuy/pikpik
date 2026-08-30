import { useState } from 'react';
import { Modal } from '../../components/ui/Modal';
import { Button } from '../../components/ui/Button';
import { FileText, Plus, RefreshCw } from 'lucide-react';

export interface EnvBulkPasteModalProps {
  isOpen: boolean;
  onClose: () => void;
  onApply: (envMap: Record<string, string>, mode: 'merge' | 'replace') => void;
}

export function EnvBulkPasteModal({ isOpen, onClose, onApply }: EnvBulkPasteModalProps) {
  const [rawText, setRawText] = useState('');
  const [mode, setMode] = useState<'merge' | 'replace'>('merge');

  const parseEnv = (text: string): Record<string, string> => {
    const result: Record<string, string> = {};
    const lines = text.split('\n');

    for (let line of lines) {
      line = line.trim();
      if (!line || line.startsWith('#')) continue;

      if (line.startsWith('export ')) {
        line = line.substring(7).trim();
      }

      const eqIdx = line.indexOf('=');
      if (eqIdx <= 0) continue;

      const key = line.substring(0, eqIdx).trim().toUpperCase();
      let value = line.substring(eqIdx + 1).trim();

      // Strip surrounding quotes
      if (
        (value.startsWith('"') && value.endsWith('"')) ||
        (value.startsWith("'") && value.endsWith("'"))
      ) {
        value = value.substring(1, value.length - 1);
      }

      if (key) {
        result[key] = value;
      }
    }

    return result;
  };

  const parsed = parseEnv(rawText);
  const parsedCount = Object.keys(parsed).length;

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (parsedCount === 0) return;
    onApply(parsed, mode);
    setRawText('');
    onClose();
  };

  return (
    <Modal
      isOpen={isOpen}
      onClose={onClose}
      title={
        <div className="flex items-center gap-2">
          <FileText className="h-5 w-5 text-cyan-400" />
          <span>Bulk Paste Environment (.env)</span>
        </div>
      }
      description="Paste multi-line KEY=VALUE pairs to bulk import variables and secrets"
      size="lg"
    >
      <form onSubmit={handleSubmit} className="space-y-4">
        <div className="space-y-1.5">
          <div className="flex items-center justify-between text-xs text-zinc-400">
            <label className="font-medium text-zinc-300">Environment Variables Definition</label>
            <span className="font-mono text-[11px] text-cyan-400">
              {parsedCount} {parsedCount === 1 ? 'variable' : 'variables'} detected
            </span>
          </div>
          <textarea
            value={rawText}
            onChange={(e) => setRawText(e.target.value)}
            rows={10}
            className="w-full rounded-lg border border-zinc-800 bg-zinc-950 font-mono text-xs text-zinc-200 p-3 leading-relaxed focus:border-cyan-500 focus:ring-1 focus:ring-cyan-500 focus:outline-none placeholder-zinc-600"
            placeholder={`# Paste your .env contents here\nNODE_ENV=production\nDATABASE_URL=postgres://user:pass@db:5432/mydb\nAPI_KEY="sk_live_123456789"\nPORT=8080`}
            autoFocus
          />
        </div>

        {/* Mode Selector */}
        <div className="p-3 bg-zinc-950/80 rounded-lg border border-zinc-800 space-y-2">
          <span className="text-xs font-medium text-zinc-300 block">Import Mode</span>
          <div className="grid grid-cols-2 gap-2">
            <button
              type="button"
              onClick={() => setMode('merge')}
              className={`p-2 rounded-md border text-left text-xs transition-colors flex items-center gap-2 ${
                mode === 'merge'
                  ? 'border-cyan-500/80 bg-cyan-950/30 text-cyan-300 font-medium'
                  : 'border-zinc-800 bg-zinc-900/60 text-zinc-400 hover:text-zinc-200'
              }`}
            >
              <Plus className="h-3.5 w-3.5" />
              <div>
                <div className="font-semibold text-zinc-200">Merge with existing</div>
                <div className="text-[10px] text-zinc-400">Keeps untouched keys, updates matched</div>
              </div>
            </button>

            <button
              type="button"
              onClick={() => setMode('replace')}
              className={`p-2 rounded-md border text-left text-xs transition-colors flex items-center gap-2 ${
                mode === 'replace'
                  ? 'border-amber-500/80 bg-amber-950/30 text-amber-300 font-medium'
                  : 'border-zinc-800 bg-zinc-900/60 text-zinc-400 hover:text-zinc-200'
              }`}
            >
              <RefreshCw className="h-3.5 w-3.5" />
              <div>
                <div className="font-semibold text-zinc-200">Replace all</div>
                <div className="text-[10px] text-zinc-400">Overwrites all existing variables</div>
              </div>
            </button>
          </div>
        </div>

        <div className="flex justify-end gap-2 pt-3 border-t border-zinc-800">
          <Button type="button" variant="outline" size="sm" onClick={onClose}>
            Cancel
          </Button>
          <Button
            type="submit"
            variant="primary"
            size="sm"
            disabled={parsedCount === 0}
          >
            Import {parsedCount > 0 ? `${parsedCount} Variables` : ''}
          </Button>
        </div>
      </form>
    </Modal>
  );
}
