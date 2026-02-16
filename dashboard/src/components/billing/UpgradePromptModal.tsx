import { CreditCard } from 'lucide-react';
import { useNavigate } from 'react-router-dom';
import { billing } from '../../lib/api';
import { toast } from 'sonner';
import { Button } from '../ui/Button';
import { Modal } from '../ui/Modal';

export function UpgradePromptModal({
  open,
  title,
  message,
  onClose,
}: {
  open: boolean;
  title?: string;
  message: string;
  onClose: () => void;
}) {
  const navigate = useNavigate();

  const handleAddPaymentMethod = async () => {
    try {
      const { url } = await billing.createCheckoutSession(`${window.location.origin}/billing/plans`);
      window.location.href = url;
    } catch {
      toast.error('Failed to open checkout');
    }
  };

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={title || 'Upgrade Required'}
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>Not now</Button>
          <Button variant="secondary" onClick={() => { onClose(); navigate('/billing/plans'); }}>
            View plans
          </Button>
          <Button onClick={() => { onClose(); handleAddPaymentMethod(); }}>
            <CreditCard className="w-4 h-4 mr-2" />
            Add payment method
          </Button>
        </>
      }
    >
      <p className="text-sm text-content-secondary">{message}</p>
    </Modal>
  );
}

