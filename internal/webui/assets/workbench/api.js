// 请求封装只报告连接变化，不直接修改页面或编辑状态。
export function createAPI({ onConnection }) {
  return async function api(path, options = {}) {
    const init = {
      method: options.method || "GET",
      credentials: "same-origin",
      cache: "no-store",
      ...options,
    };
    if (init.body !== undefined) {
      init.headers = {
        "Content-Type": "application/json",
        ...(init.headers || {}),
      };
      if (typeof init.body !== "string") init.body = JSON.stringify(init.body);
    }
    let response;
    try {
      response = await fetch("/api/v1/" + path, init);
    } catch (e) {
      onConnection("连接已断开");
      throw e;
    }
    const data = await response.json();
    if (!response.ok) {
      const e = new Error(data.error || `请求失败 ${response.status}`);
      e.status = response.status;
      e.data = data;
      throw e;
    }
    onConnection("已连接");
    return data;
  };
}
