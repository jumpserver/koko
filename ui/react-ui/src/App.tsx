import { ConfigProvider } from 'antd';
import { LayoutComponent } from './layouts/index';

export const App = () => {
  return (
    <ConfigProvider
      theme={{
        components: {
          Drawer: {}
        }
      }}
    >
      <LayoutComponent />
    </ConfigProvider>
  );
};
