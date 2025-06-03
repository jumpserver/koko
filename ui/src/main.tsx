import { App } from './App.tsx';
import { createRoot } from 'react-dom/client';

import './lang/index.ts';
import './index.css';
import '@xterm/xterm/css/xterm.css';
import '@ant-design/v5-patch-for-react-19';

createRoot(document.getElementById('root')!).render(<App />);
