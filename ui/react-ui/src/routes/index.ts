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

              // console.log('i18n实例情况:', {
              //   初始化完成: i18n.isInitialized,
              //   当前语言: i18n.language,
              //   可用语言: i18n.languages,
              //   翻译资源: Object.keys(i18n.store.data)
              // });
              // console.log('完整的zh-hans翻译资源:', JSON.stringify(i18n.getResourceBundle('zh-hans', 'translation')));
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
    },
    {
      path: '/editor',
      lazy: async () => {
        const { default: Editor } = await import('@/pages/editor');

        return {
          Component: Editor
        };
      }
    }
  ],
  {
    basename: '/koko'
  }
);

export default router;
