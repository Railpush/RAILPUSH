import { useEffect, useState } from 'react';
import { useParams } from 'react-router-dom';
import { HardDrive } from 'lucide-react';
import { Button } from '../components/ui/Button';
import { Card } from '../components/ui/Card';
import { Input } from '../components/ui/Input';
import { Modal } from '../components/ui/Modal';
import { EmptyState } from '../components/ui/EmptyState';
import { disks as disksApi } from '../lib/api';
import type { Disk } from '../types';

export function ServiceDisks() {
  const { serviceId } = useParams<{ serviceId: string }>();
  const [disks, setDisks] = useState<Disk[]>([]);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const [showAdd, setShowAdd] = useState(false);
  const [newDisk, setNewDisk] = useState({ name: '', mount_path: '/var/data', size_gb: 10 });

  const loadDisks = async () => {
    if (!serviceId) return;
    setLoading(true);
    setError('');
    try {
      const list = await disksApi.list(serviceId);
      setDisks(Array.isArray(list) ? list : []);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load disks');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadDisks();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [serviceId]);

  const addDisk = async () => {
    if (!serviceId) return;
    setSaving(true);
    setError('');
    try {
      await disksApi.upsert(serviceId, {
        name: newDisk.name,
        mount_path: newDisk.mount_path,
        size_gb: newDisk.size_gb,
      });
      setShowAdd(false);
      setNewDisk({ name: '', mount_path: '/var/data', size_gb: 10 });
      await loadDisks();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to save disk');
    } finally {
      setSaving(false);
    }
  };

  const removeDisk = async () => {
    if (!serviceId) return;
    setSaving(true);
    setError('');
    try {
      await disksApi.remove(serviceId);
      await loadDisks();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to delete disk');
    } finally {
      setSaving(false);
    }
  };

  return (
    <div>
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-semibold text-content-primary">Disks</h1>
        <Button onClick={() => setShowAdd(true)} disabled={saving}>{disks.length > 0 ? 'Replace Disk' : 'Add Disk'}</Button>
      </div>

      {error && (
        <div className="mb-4 rounded-lg border border-status-danger/30 bg-status-danger/10 px-3 py-2 text-sm text-status-danger">
          {error}
        </div>
      )}

      {loading ? (
        <div className="text-sm text-content-secondary">Loading disks...</div>
      ) : disks.length === 0 ? (
        <EmptyState
          icon={<HardDrive className="w-6 h-6" />}
          title="No persistent disk attached"
          description="Attach a disk to persist uploads or local data across deploys. Redeploy service after changes to apply mounts."
          action={{ label: 'Add Disk', onClick: () => setShowAdd(true) }}
        />
      ) : (
        <div className="space-y-3">
          {disks.map((disk) => (
            <Card key={disk.id}>
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-3">
                  <div className="w-8 h-8 rounded-md bg-brand/10 flex items-center justify-center">
                    <HardDrive className="w-4 h-4 text-brand" />
                  </div>
                  <div>
                    <div className="text-sm font-semibold text-content-primary">{disk.name}</div>
                    <div className="text-xs text-content-secondary">
                      {disk.mount_path} &middot; {disk.size_gb} GB
                    </div>
                  </div>
                </div>
                <Button variant="danger" onClick={removeDisk} disabled={saving}>Delete</Button>
              </div>
            </Card>
          ))}
          <p className="text-xs text-content-tertiary">Changes to disk mounts require a service redeploy.</p>
        </div>
      )}

      <Modal
        open={showAdd}
        onClose={() => setShowAdd(false)}
        title="Add Persistent Disk"
        footer={
          <>
            <Button variant="secondary" onClick={() => setShowAdd(false)}>Cancel</Button>
            <Button onClick={addDisk} disabled={!newDisk.name || saving}>{saving ? 'Saving...' : 'Save Disk'}</Button>
          </>
        }
      >
        <div className="space-y-4">
          <Input label="Name" value={newDisk.name} onChange={(e) => setNewDisk({ ...newDisk, name: e.target.value })} placeholder="uploads" />
          <Input label="Mount Path" value={newDisk.mount_path} onChange={(e) => setNewDisk({ ...newDisk, mount_path: e.target.value })} placeholder="/var/data" />
          <Input label="Size (GB)" type="number" value={String(newDisk.size_gb)} onChange={(e) => setNewDisk({ ...newDisk, size_gb: Number(e.target.value) })} />
          <p className="text-xs text-content-tertiary">
            Services with persistent disks cannot use zero-downtime deploys.
          </p>
        </div>
      </Modal>
    </div>
  );
}
