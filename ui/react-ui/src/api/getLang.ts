import { get } from './index';
import { getConnectionUrl, getLang } from '@/utils';

export const getTranslations = () => {
  const url = getConnectionUrl('http');
  const lang = getLang();

  return get(`${url}/api/v1/settings/i18n/koko/?lang=${lang}&flat=0`);
};
