import { ref } from 'vue';
import { NInput } from 'naive-ui';
import { useI18n } from 'vue-i18n';

export function dialogContent() {
  const { t } = useI18n();

  const verifyValue = ref('');

  return {
    render: () => (
      <NInput
        clearable
        size="small"
        type="password"
        show-password-on="mousedown"
        value={verifyValue.value}
        onUpdateValue={(val: string) => (verifyValue.value = val)}
        placeholder={t('InputVerifyCode')}
      />
    ),
    getValue: () => verifyValue.value,
  };
}
