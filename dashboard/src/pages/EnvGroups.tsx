import { useState, useEffect } from 'react';
import { Plus, Link2, Settings } from 'lucide-react';
import { Button } from '../components/ui/Button';
import { Card } from '../components/ui/Card';
import { EmptyState } from '../components/ui/EmptyState';
import { Modal } from '../components/ui/Modal';
import { Input } from '../components/ui/Input';
import { envGroups as egApi } from '../lib/api';
import type { EnvGroup } from '../types';
import { toast } from 'sonner';

export function EnvGroups() {
  const [groups, setGroups] = useState<EnvGroup[]>([]);
  const [, setLoading] = useState(true);
  const [showCreate, setShowCreate] = useState(false);
  const [newName, setNewName] = useState('');

  useEffect(() => {
    egApi.list().then(setGroups).catch(() => {}).finally(() => setLoading(false));
  }, []);

  const createGroup = async () => {
    if (!newName.trim()) return;
    try {
      const g = await egApi.create({ name: newName });
      setGroups([...groups, g]);
      setShowCreate(false);
      setNewName('');
      toast.success('Environment group created');
    } catch {
      toast.error('Failed to create group');
    }
  };

  return (
    <div>
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-semibold text-content-primary">Environment Groups</h1>
        <Button onClick={() => setShowCreate(true)}>
          <Plus className="w-4 h-4" />
          New Environment Group
        </Button>
      </div>

      {groups.length === 0 ? (
        <EmptyState
          icon={<Link2 className="w-6 h-6" />}
          title="No environment groups"
          description="Environment groups let you share environment variables across multiple services."
          action={{ label: 'Create Group', onClick: () => setShowCreate(true) }}
        />
      ) : (
        <div className="space-y-3">
          {groups.map((g) => (
            <Card key={g.id} hover>
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-3">
                  <div className="w-8 h-8 rounded-md bg-brand/10 flex items-center justify-center">
                    <Settings className="w-4 h-4 text-brand" />
                  </div>
                  <div>
                    <div className="text-sm font-semibold text-content-primary">{g.name}</div>
                    <div className="text-xs text-content-secondary mt-0.5">
                      {g.env_vars?.length || 0} variables
                    </div>
                  </div>
                </div>
              </div>
            </Card>
          ))}
        </div>
      )}

      <Modal
        open={showCreate}
        onClose={() => setShowCreate(false)}
        title="Create Environment Group"
        footer={
          <>
            <Button variant="secondary" onClick={() => setShowCreate(false)}>Cancel</Button>
            <Button onClick={createGroup} disabled={!newName.trim()}>Create</Button>
          </>
        }
      >
        <Input
          label="Name"
          value={newName}
          onChange={(e) => setNewName(e.target.value)}
          placeholder="e.g., shared-config"
        />
      </Modal>
    </div>
  );
}
