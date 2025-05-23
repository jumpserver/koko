import { useState } from 'react';
import { Input, Modal, Flex } from 'antd';

import type { GetProps } from 'antd';

import useDetail from '@/store/useDetail';
import TerminalComponent from '@/components/Terminal';

type OTPProps = GetProps<typeof Input.OTP>;

const Share: React.FC = () => {
  const { setShareInfo } = useDetail();
  const [isModalOpen, setIsModalOpen] = useState(true);

  const onChange: OTPProps['onChange'] = text => {
    setShareInfo({ shareCode: text });

    setIsModalOpen(false);
  };

  const sharedProps: OTPProps = {
    onChange
  };

  return (
    <>
      <Modal
        title="会话分享"
        centered
        closable={false}
        maskClosable={false}
        open={isModalOpen}
        cancelButtonProps={{ style: { display: 'none' } }}
        okButtonProps={{ style: { display: 'none' } }}
      >
        <Flex align="center" justify="center" className="w-full">
          <Input.OTP size="large" length={4} {...sharedProps} />
        </Flex>
      </Modal>

      {!isModalOpen && <TerminalComponent />}
    </>
  );
};

export default Share;
