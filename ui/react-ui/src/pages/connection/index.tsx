import { useLayoutEffect } from 'react';
import { useTranslation } from 'react-i18next';

import TerminalComponent from '@/components/Terminal';

const Connection: React.FC = () => {
  const { t, i18n } = useTranslation();

  useLayoutEffect(() => {
    
  })

  return (
    <div>
      <TerminalComponent />
    </div>
  );
};

export default Connection;
