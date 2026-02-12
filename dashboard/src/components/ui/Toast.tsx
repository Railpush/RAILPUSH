import { Toaster } from 'sonner';

export function ToastProvider() {
  return (
    <Toaster
      position="bottom-right"
      toastOptions={{
        style: {
          background: '#1E1E2A',
          border: '1px solid #2A2A3A',
          color: '#F1F1F3',
          fontSize: '14px',
        },
      }}
      theme="dark"
    />
  );
}
