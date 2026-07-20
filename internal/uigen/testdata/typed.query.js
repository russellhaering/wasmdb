function handler(params) {
  var t = db.table("orders");
  var docs;
  if (params && params.q) {
    docs = t.search.text(String(params.q), 50, 0);
  } else {
    docs = t.list(50);
  }
  var rows = [];
  for (var i = 0; i < docs.length; i++) {
    var d = docs[i];
    var a = d.attributes || {};
    rows.push({
      id: d.id,
      "customer": a["customer"],
      "quantity": a["quantity"],
      "total": a["total"],
      "paid": a["paid"],
      "created": a["created"],
      "tags": a["tags"],
      "owner": a["owner"]
    });
  }
  return { rows: rows, count: rows.length };
}
