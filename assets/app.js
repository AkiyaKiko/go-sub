(function () {
  var routeRulesState = [];
  var selectedRouteRuleID = "";

  function showMessage(text, isError) {
    var status = document.getElementById("status");
    if (!status) {
      return;
    }
    status.innerHTML = "";
    if (!text) {
      return;
    }
    var message = document.createElement("p");
    message.className = "message";
    if (isError) {
      message.classList.add("error");
    }
    message.textContent = text;
    status.appendChild(message);
  }

  function setText(id, value) {
    var node = document.getElementById(id);
    if (node) {
      node.textContent = value;
    }
  }

  function setValue(id, value) {
    var node = document.getElementById(id);
    if (node) {
      node.value = value;
    }
  }

  function updateDashboard(data) {
    var selected = selectedRouteRuleID;
    var select = document.getElementById("route-rule-select");
    if (select && select.value) {
      selected = select.value;
    }

    showMessage(data.message || "");
    setText("subscription-url", data.subscription_url || "");
    setText("subscription-path", data.subscription_path || "");
    setText("nodes-count", String(data.node_count || 0));
    setValue("path-input", data.subscription_path || "");
    setValue("nodes-textarea", data.nodes_text || "");
    renderRouteRules(data.route_rules || [], selected);
  }

  function appendField(parent, labelText, field) {
    var label = document.createElement("label");
    var labelSpan = document.createElement("span");
    labelSpan.className = "label";
    labelSpan.textContent = labelText;
    label.appendChild(labelSpan);
    label.appendChild(field);
    parent.appendChild(label);
  }

  function createInput(name, value, placeholder) {
    var input = document.createElement("input");
    input.name = name;
    input.value = value || "";
    input.placeholder = placeholder || "";
    input.required = true;
    return input;
  }

  function createTextarea(name, value) {
    var textarea = document.createElement("textarea");
    textarea.name = name;
    textarea.spellcheck = false;
    textarea.value = value || "";
    return textarea;
  }

  function createButton(className, name, value, text) {
    var button = document.createElement("button");
    button.className = className;
    button.type = "submit";
    if (name) {
      button.name = name;
      button.value = value;
    }
    button.textContent = text;
    return button;
  }

  function findRouteRule(routeRules, id) {
    for (var i = 0; i < routeRules.length; i += 1) {
      if (String(routeRules[i].id) === String(id)) {
        return routeRules[i];
      }
    }
    return null;
  }

  function renderRouteRuleOptions(routeRules, selectedID) {
    var select = document.getElementById("route-rule-select");
    if (!select) {
      return "";
    }

    select.innerHTML = "";
    if (!routeRules.length) {
      var emptyOption = document.createElement("option");
      emptyOption.value = "";
      emptyOption.textContent = "暂无路由规则";
      select.appendChild(emptyOption);
      select.disabled = true;
      selectedRouteRuleID = "";
      return "";
    }

    var selectedRule = findRouteRule(routeRules, selectedID) || routeRules[0];
    selectedRouteRuleID = String(selectedRule.id);
    select.disabled = false;
    routeRules.forEach(function (rule) {
      var option = document.createElement("option");
      option.value = String(rule.id);
      option.textContent = (rule.name || "路由规则") + " - " + (rule.path || "");
      if (String(rule.id) === selectedRouteRuleID) {
        option.selected = true;
      }
      select.appendChild(option);
    });
    return selectedRouteRuleID;
  }

  function createRouteRuleEditor(rule) {
    var card = document.createElement("article");
    card.className = "route-rule-card";

    var form = document.createElement("form");
    form.className = "route-rule-form";
    form.method = "post";
    form.setAttribute("action", "/route-rules/" + rule.id);
    form.setAttribute("data-ajax-form", "");

    var grid = document.createElement("div");
    grid.className = "route-rule-grid";
    appendField(grid, "名称", createInput("name", rule.name, "路由规则"));
    appendField(grid, "路径", createInput("path", rule.path, "/rule-path"));
    form.appendChild(grid);

    var info = document.createElement("div");
    info.className = "info-item";
    var infoLabel = document.createElement("span");
    infoLabel.className = "label";
    infoLabel.textContent = "访问 URL";
    var url = document.createElement("div");
    url.className = "value";
    url.textContent = rule.url || "";
    info.appendChild(infoLabel);
    info.appendChild(url);
    form.appendChild(info);

    appendField(form, "JSON 内容", createTextarea("content", rule.content));

    var footer = document.createElement("div");
    footer.className = "footer-row";
    var id = document.createElement("span");
    id.className = "muted";
    id.textContent = "ID " + rule.id;
    var actions = document.createElement("div");
    actions.className = "button-row";
    actions.appendChild(createButton("ghost", "submit_action", "random", "随机路径并保存"));
    actions.appendChild(createButton("primary", "submit_action", "save", "保存规则"));
    footer.appendChild(id);
    footer.appendChild(actions);
    form.appendChild(footer);

    var deleteForm = document.createElement("form");
    deleteForm.className = "route-rule-delete";
    deleteForm.method = "post";
    deleteForm.setAttribute("action", "/route-rules/" + rule.id + "/delete");
    deleteForm.setAttribute("data-ajax-form", "");
    deleteForm.setAttribute("data-confirm", "确认删除这条路由规则？");
    deleteForm.appendChild(createButton("danger", "", "", "删除规则"));

    card.appendChild(form);
    card.appendChild(deleteForm);
    bindAjaxForm(form);
    bindAjaxForm(deleteForm);
    return card;
  }

  function renderRouteRuleEditor(selectedID) {
    var editor = document.getElementById("route-rule-editor");
    if (!editor) {
      return;
    }

    editor.innerHTML = "";
    if (!routeRulesState.length) {
      var empty = document.createElement("p");
      empty.className = "muted empty-state";
      empty.textContent = "还没有路由规则。";
      editor.appendChild(empty);
      return;
    }

    var rule = findRouteRule(routeRulesState, selectedID) || routeRulesState[0];
    selectedRouteRuleID = String(rule.id);
    editor.appendChild(createRouteRuleEditor(rule));
  }

  function renderRouteRules(routeRules, selectedID) {
    routeRulesState = routeRules.slice();
    var nextSelectedID = renderRouteRuleOptions(routeRulesState, selectedID);
    renderRouteRuleEditor(nextSelectedID);
  }

  function loadInitialRouteRules() {
    var dataNode = document.getElementById("route-rules-data");
    if (!dataNode || !dataNode.dataset.rules) {
      return [];
    }
    try {
      return JSON.parse(window.atob(dataNode.dataset.rules));
    } catch (error) {
      return [];
    }
  }

  function resetNewRouteRuleForm(form, data) {
    if (form.id !== "new-route-rule-form") {
      return;
    }
    form.reset();
    var content = form.querySelector("textarea[name='content']");
    if (content) {
      content.value = data.default_route_json || "{\n  \"rules\": []\n}";
    }
  }

  function bindAjaxForm(form) {
    if (form.dataset.bound === "true") {
      return;
    }
    form.dataset.bound = "true";
    form.addEventListener("submit", function (event) {
      event.preventDefault();
      if (form.dataset.confirm && !window.confirm(form.dataset.confirm)) {
        return;
      }

      var submitter = event.submitter;
      var formData = new FormData(form);
      if (submitter && submitter.name) {
        formData.set(submitter.name, submitter.value);
      }

      var action = form.getAttribute("action") || window.location.href;

      fetch(action, {
        method: form.method || "POST",
        body: new URLSearchParams(formData),
        credentials: "same-origin",
        headers: {
          Accept: "application/json",
          "Content-Type": "application/x-www-form-urlencoded;charset=UTF-8"
        }
      })
        .then(function (response) {
          return response.json().then(function (data) {
            if (!response.ok) {
              throw data;
            }
            return data;
          });
        })
        .then(function (data) {
          updateDashboard(data);
          resetNewRouteRuleForm(form, data);
        })
        .catch(function (error) {
          showMessage(error && error.message ? error.message : "保存失败", true);
        });
    });
  }

  document.addEventListener("DOMContentLoaded", function () {
    document.querySelectorAll("form[data-ajax-form]").forEach(bindAjaxForm);
    var select = document.getElementById("route-rule-select");
    if (select) {
      selectedRouteRuleID = select.value || "";
      routeRulesState = loadInitialRouteRules();
      renderRouteRuleEditor(selectedRouteRuleID);
      select.addEventListener("change", function () {
        selectedRouteRuleID = select.value || "";
        renderRouteRuleEditor(selectedRouteRuleID);
      });
    }
  });
})();
