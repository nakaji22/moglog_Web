// もぐログ フロント側スクリプト（食事フォームの品目編集のみ）
(function () {
  "use strict";
  var form = document.getElementById("meal-form");
  if (!form) return;

  function parseJSON(id) {
    var el = document.getElementById(id);
    if (!el) return [];
    try { return JSON.parse(el.textContent || "[]"); } catch (e) { return []; }
  }
  var FOODS = parseJSON("foods-data");
  var TEMPLATES = parseJSON("templates-data");
  var INITIAL = parseJSON("initial-items");
  var foodById = {};
  FOODS.forEach(function (f) { foodById[f.id] = f; });

  var tbody = document.getElementById("items-body");
  var NUTRIENTS = ["Kcal", "Protein", "Fat", "Carbs", "Fiber", "Salt"];
  var FIELD = { Kcal: "item_kcal", Protein: "item_protein", Fat: "item_fat", Carbs: "item_carbs", Fiber: "item_fiber", Salt: "item_salt" };

  function roundFor(key, v) {
    if (key === "Kcal") return Math.round(v);
    return Math.round(v * 10) / 10;
  }

  function makeInput(name, type, value, step) {
    var input = document.createElement("input");
    input.type = type;
    input.name = name;
    input.value = value;
    if (step) input.step = step;
    if (type === "number") input.min = "0";
    return input;
  }

  function addRow(item) {
    var tr = document.createElement("tr");
    tr.dataset.per100 = item.per100 ? JSON.stringify(item.per100) : "";

    var tdName = document.createElement("td");
    tdName.className = "col-name";
    var hidden = makeInput("food_id", "hidden", item.foodId || "");
    var nameInput = makeInput("item_name", "text", item.name || "");
    nameInput.placeholder = "品目名";
    tdName.appendChild(hidden);
    tdName.appendChild(nameInput);
    tr.appendChild(tdName);

    var tdGrams = document.createElement("td");
    var gramsInput = makeInput("item_grams", "number", item.grams || 0, "1");
    tdGrams.appendChild(gramsInput);
    tr.appendChild(tdGrams);

    var nutInputs = {};
    NUTRIENTS.forEach(function (key) {
      var td = document.createElement("td");
      var v = item.nutrients ? item.nutrients[key] || 0 : 0;
      var inp = makeInput(FIELD[key], "number", v, key === "Kcal" ? "1" : "0.1");
      nutInputs[key] = inp;
      td.appendChild(inp);
      tr.appendChild(td);
    });

    var tdDel = document.createElement("td");
    var del = document.createElement("button");
    del.type = "button";
    del.className = "btn btn-text btn-small";
    del.textContent = "削除";
    del.setAttribute("aria-label", "この品目を削除");
    del.addEventListener("click", function () {
      tr.remove();
      updateTotals();
    });
    tdDel.appendChild(del);
    tr.appendChild(tdDel);

    gramsInput.addEventListener("input", function () {
      if (!tr.dataset.per100) { updateTotals(); return; }
      var per100 = JSON.parse(tr.dataset.per100);
      var g = parseFloat(gramsInput.value) || 0;
      NUTRIENTS.forEach(function (key) {
        nutInputs[key].value = roundFor(key, (per100[key] || 0) * g / 100);
      });
      updateTotals();
    });
    Object.keys(nutInputs).forEach(function (key) {
      nutInputs[key].addEventListener("input", updateTotals);
    });
    nameInput.addEventListener("input", updateTotals);

    tbody.appendChild(tr);
    updateTotals();
    return tr;
  }

  function updateTotals() {
    var sums = { Kcal: 0, Protein: 0, Fat: 0, Carbs: 0, Fiber: 0, Salt: 0 };
    tbody.querySelectorAll("tr").forEach(function (tr) {
      NUTRIENTS.forEach(function (key) {
        var inp = tr.querySelector('input[name="' + FIELD[key] + '"]');
        if (inp) sums[key] += parseFloat(inp.value) || 0;
      });
    });
    document.getElementById("total-kcal").textContent = Math.round(sums.Kcal);
    document.getElementById("total-protein").textContent = (Math.round(sums.Protein * 10) / 10).toFixed(1);
    document.getElementById("total-fat").textContent = (Math.round(sums.Fat * 10) / 10).toFixed(1);
    document.getElementById("total-carbs").textContent = (Math.round(sums.Carbs * 10) / 10).toFixed(1);
    document.getElementById("total-fiber").textContent = (Math.round(sums.Fiber * 10) / 10).toFixed(1);
    document.getElementById("total-salt").textContent = (Math.round(sums.Salt * 10) / 10).toFixed(1);
  }

  function addFoodRow(food, grams) {
    var g = grams || food.defaultGrams || 100;
    var nutrients = {};
    NUTRIENTS.forEach(function (key) {
      nutrients[key] = roundFor(key, (food.per100[key] || 0) * g / 100);
    });
    return addRow({ foodId: food.id, name: food.name, grams: g, per100: food.per100, nutrients: nutrients });
  }

  // 初期行
  if (INITIAL.length > 0) {
    INITIAL.forEach(function (it) { addRow(it); });
  } else {
    addRow({ foodId: "", name: "", grams: 100, per100: null, nutrients: {} });
  }

  // 食品検索
  var search = document.getElementById("food-search");
  var results = document.getElementById("food-results");
  if (search && results) {
    search.addEventListener("input", function () {
      var q = search.value.trim();
      results.innerHTML = "";
      if (!q) return;
      var matched = FOODS.filter(function (f) {
        if (f.name.indexOf(q) >= 0 || f.category.indexOf(q) >= 0) return true;
        return (f.aliases || []).some(function (a) { return a.indexOf(q) >= 0; });
      }).slice(0, 12);
      matched.forEach(function (f) {
        var btn = document.createElement("button");
        btn.type = "button";
        btn.setAttribute("role", "option");
        btn.textContent = f.name + " ";
        var meta = document.createElement("span");
        meta.className = "food-meta";
        meta.textContent = f.category + " / " + Math.round(f.per100.Kcal) + " kcal（100g）";
        btn.appendChild(meta);
        btn.addEventListener("click", function () {
          addFoodRow(f);
          results.innerHTML = "";
          search.value = "";
          search.focus();
        });
        results.appendChild(btn);
      });
    });
    document.addEventListener("click", function (ev) {
      if (!results.contains(ev.target) && ev.target !== search) results.innerHTML = "";
    });
  }

  // 手入力で追加
  var addCustom = document.getElementById("add-custom");
  if (addCustom) {
    addCustom.addEventListener("click", function () {
      var tr = addRow({ foodId: "", name: "", grams: 100, per100: null, nutrients: {} });
      var nameInput = tr.querySelector('input[name="item_name"]');
      if (nameInput) nameInput.focus();
    });
  }

  // よく記録する食品チップ
  document.querySelectorAll("[data-add-food]").forEach(function (btn) {
    btn.addEventListener("click", function () {
      var food = foodById[btn.getAttribute("data-add-food")];
      if (food) addFoodRow(food);
    });
  });

  // テンプレート適用
  document.querySelectorAll("[data-apply-template]").forEach(function (btn) {
    btn.addEventListener("click", function () {
      var id = btn.getAttribute("data-apply-template");
      var tpl = null;
      TEMPLATES.forEach(function (t) { if (t.id === id) tpl = t; });
      if (!tpl) return;
      tbody.innerHTML = "";
      (tpl.items || []).forEach(function (it) {
        var food = foodById[it.foodId];
        if (food) addFoodRow(food, it.grams);
      });
      var title = document.getElementById("title");
      if (title && !title.value) title.value = tpl.title;
      var src = document.getElementById("source-field");
      if (src && src.value !== "photo") src.value = "template";
      updateTotals();
    });
  });
})();
