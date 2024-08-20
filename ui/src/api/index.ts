import { createAlova } from 'alova';
import fetchAdapter from 'alova/fetch';

export const alovaInstance = createAlova({
    requestAdapter: fetchAdapter()
});
