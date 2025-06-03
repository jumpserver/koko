import i18n from '@/lang';
import { getLang } from '@/utils';
import { getTranslations } from '@/api/getLang';
import { LayoutComponent } from '@/layouts/index';
import { createBrowserRouter } from 'react-router';

const router = createBrowserRouter(
  [
    {
      Component: LayoutComponent,
      children: [
        {
          path: '/connect',
          lazy: async () => {
            const { default: Connection } = await import('@/pages/connection');

            return {
              Component: Connection
            };
          },
          loader: async () => {
            // 加载前获取 lang 信息
            const lang = getLang();
            const translations = (await getTranslations()) as Record<string, string>;

            if (translations[lang]) {
              i18n.addResourceBundle(lang, 'translation', translations[lang], true, true);
              i18n.changeLanguage(lang);
            }

            return {
              success: true
            };
          }
        }
      ]
    },
    {
      path: '/share',
      lazy: async () => {
        const { default: Share } = await import('@/pages/share');

        return {
          Component: Share
        };
      }
    }
  ],
  {
    basename: '/koko'
  }
);

export default router;
