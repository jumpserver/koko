import type { LucideProps } from 'lucide-react';

export interface DrawerItem {
  label: string;

  value: string;

  component: React.ReactNode;

  icon: React.ForwardRefExoticComponent<Omit<LucideProps, 'ref'>>;
}

export interface SidebarProps {
  open: boolean;

  width: number;

  setOpen: (open: boolean) => void;
}
