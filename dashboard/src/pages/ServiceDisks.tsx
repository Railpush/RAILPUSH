import { useState } from 'react';
import { HardDrive } from 'lucide-react';
import { Button } from '../components/ui/Button';
import { Card } from '../components/ui/Card';
import { Input } from '../components/ui/Input';
import { Modal } from '../components/ui/Modal';
import { EmptyState } from '../components/ui/EmptyState';
import type { Disk } from '../types';

export function ServiceDisks() {
  const [disks, setDisks] = useState<Disk[]>([]);
  const [showAdd, setShowAdd] = useState(false);
  const [newDisk, setNewDisk] = useState({ name: '', mount_path: '/var/data', size_gb: 10 });

  const addDisk = () => {
    setDisks([...disks, {
      id: crypto.randomUUID(),
      service_id: '',
      name: newDisk.name,
      mount_path: newDisk.mount_path,
      size_gb: newDisk.size_gb,
      created_at: new Date().toISOString(),
    }]);
    setShowAdd(false);
    setNewDisk({ name: '', mount_path: '/var/data', size_gb: 10 });
  };

  return (
    <div>
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-semibold text-content-primary">Disks</h1>
        <span className="text-xs font-medium text-amber-500 bg-amber-500/10 px-2.5 py-1 rounded-full">Coming Soon</span>
      </div>

      {disks.length === 0 ? (
        <EmptyState
          icon={<HardDrive className="w-6 h-6" />}
          title="Persistent disks coming soon"
          description="Persistent disk support is under development. This feature will let you attach durable storage that persists across deploys."
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
              </div>
            </Card>
          ))}
        </div>
      )}

      <Modal
        open={showAdd}
        onClose={() => setShowAdd(false)}
        title="Add Persistent Disk"
        footer={
          <>
            <Button variant="secondary" onClick={() => setShowAdd(false)}>Cancel</Button>
            <Button onClick={addDisk} disabled={!newDisk.name}>Add Disk</Button>
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
