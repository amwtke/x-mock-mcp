const element = id => document.getElementById(id);
const money = cents => new Intl.NumberFormat('zh-CN', {style: 'currency', currency: 'CNY'}).format(cents / 100);
let selected;
async function request(path, options = {}) {
  const response = await fetch(path, {...options, headers: {'Content-Type': 'application/json', 'X-Test-User-Id': '2001'}});
  const body = await response.json();
  if (!response.ok) throw new Error(body.message || `请求失败（${response.status}）`);
  return body;
}
function failure(error) { element('error').textContent = error.message; element('error').hidden = false; }
async function products() {
  const rows = await request('/api/products');
  element('products').replaceChildren();
  for (const row of rows) {
    const article = document.createElement('article');
    const text = document.createElement('p'); text.textContent = `${row.name} · ${money(row.priceCents)}`;
    const button = document.createElement('button'); button.textContent = '查看详情';
    button.addEventListener('click', () => detail(row.id).catch(failure));
    article.append(text, button); element('products').append(article);
  }
}
async function detail(id) {
  selected = await request(`/api/products/${id}`);
  element('detail').hidden = false;
  element('product-name').textContent = selected.name;
  element('price').textContent = money(selected.priceCents);
  element('stock').textContent = selected.stock;
  element('place').disabled = selected.stock === 0;
}
element('order-form').addEventListener('submit', async event => {
  event.preventDefault(); element('error').hidden = true; element('receipt').hidden = true;
  element('place').disabled = true;
  try {
    const order = await request('/api/orders', {method: 'POST', body: JSON.stringify({productId: selected.id, quantity: Number(element('quantity').value)})});
    const saved = await request(`/api/orders/${order.id}`);
    element('receipt').textContent = `下单成功：订单 ${saved.id}，${saved.productName} ${saved.quantity} 件，合计 ${money(saved.totalCents)}`;
    element('receipt').hidden = false;
    await detail(selected.id);
  } catch (error) { failure(error); }
  finally { element('place').disabled = !selected || selected.stock === 0; }
});
element('query-orders').addEventListener('click', async () => {
  try {
    const rows = await request('/api/orders');
    element('orders').replaceChildren();
    if (!rows.length) element('orders').textContent = '暂无订单';
    for (const row of rows) {
      const item = document.createElement('li');
      item.textContent = `订单 ${row.id} · ${row.productName} · ${row.quantity} 件 · ${money(row.totalCents)}`;
      element('orders').append(item);
    }
  } catch (error) { failure(error); }
});
products().catch(failure);
