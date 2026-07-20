function handler(params) {
  var t = db.table("notes");
  var docs = t.list(50);
  var rows = [];
  for (var i = 0; i < docs.length; i++) {
    var d = docs[i];
    var a = d.attributes || {};
    rows.push({
      id: d.id
    });
  }
  return { rows: rows, count: rows.length };
}
