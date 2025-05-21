import { Outlet } from 'react-router';

export const MainContainer: React.FC = () => {
  return (
    <div className="w-full h-full bg-black">
      <Outlet />
    </div>
  );
};
