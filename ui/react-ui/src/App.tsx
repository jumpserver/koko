import { RouterProvider } from 'react-router';
import { App as AntApp, ConfigProvider } from 'antd';

import router from './routes';

export const App = () => {
  return (
    <ConfigProvider>
      <AntApp message={{ maxCount: 1 }} notification={{ maxCount: 1 }}>
        <RouterProvider router={router} />
      </AntApp>
    </ConfigProvider>
  );
};
