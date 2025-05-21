import axios from 'axios';

const request = axios.create({
  baseURL: import.meta.env.VITE_KOKO_API_URL,
  timeout: 5000
});

request.interceptors.request.use(config => {
  return config;
});

request.interceptors.response.use(response => {
  if (response.status === 200) {
    return response.data;
  }

  return response;
});

export const get = <T>(url: string, params?: any): Promise<T> => {
  return request.get(url, { params });
};

