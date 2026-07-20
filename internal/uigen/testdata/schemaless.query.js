function handler(params) {
  var t = db.table("posts");
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
      "title": a["title"],
      "views": a["views"],
      "published": a["published"],
      "when": a["when"]
    });
  }
  return { rows: rows, count: rows.length };
}
