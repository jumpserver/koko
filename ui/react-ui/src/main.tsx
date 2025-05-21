import { App } from './App.tsx';
import { createRoot } from 'react-dom/client';

import './lang';
import './index.css';
import '@ant-design/v5-patch-for-react-19';

createRoot(document.getElementById('root')!).render(<App />);
