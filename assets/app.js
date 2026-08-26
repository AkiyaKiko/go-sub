(function () {
  var routeRulesState = [];
  var selectedRouteRuleID = "";
  var toastSequence = 0;

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

  function showToast(text, isError) {
    var region = document.getElementById("toast-region");
    if (!region || !text) {
      return;
    }

    toastSequence += 1;
    var toast = document.createElement("div");
    toast.className = "toast";
    toast.dataset.toastId = String(toastSequence);
    toast.setAttribute("role", isError ? "alert" : "status");
    if (isError) {
      toast.classList.add("error");
    }
    toast.textContent = text;
    region.appendChild(toast);

    window.requestAnimationFrame(function () {
      toast.classList.add("visible");
    });
    window.setTimeout(function () {
      toast.classList.remove("visible");
      window.setTimeout(function () {
        toast.remove();
      }, 180);
    }, 3000);
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

  function updateDashboard(data, preferredRouteRuleID) {
    var selected = preferredRouteRuleID || selectedRouteRuleID;
    var select = document.getElementById("route-rule-select");
    if (!preferredRouteRuleID && select && select.value) {
      selected = select.value;
    }

    showMessage("");
    setText("subscription-url", data.subscription_url || "");
    setText("subscription-path", data.subscription_path || "");
    setText("nodes-count", String(data.node_count || 0));
    setText("route-rules-count", String((data.route_rules || []).length));
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

  function findAddedRouteRuleID(nextRouteRules) {
    var existingIDs = {};
    routeRulesState.forEach(function (rule) {
      existingIDs[String(rule.id)] = true;
    });
    for (var i = 0; i < nextRouteRules.length; i += 1) {
      if (!existingIDs[String(nextRouteRules[i].id)]) {
        return String(nextRouteRules[i].id);
      }
    }
    return nextRouteRules.length ? String(nextRouteRules[nextRouteRules.length - 1].id) : "";
  }

  function updateRouteRuleDeleteControl(selectedID) {
    var form = document.getElementById("delete-route-rule-form");
    var button = document.getElementById("delete-route-rule-button");
    if (!form || !button) {
      return;
    }
    if (!selectedID) {
      form.setAttribute("action", "");
      button.disabled = true;
      return;
    }
    form.setAttribute("action", "/route-rules/" + selectedID + "/delete");
    button.disabled = false;
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
      updateRouteRuleDeleteControl("");
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
    updateRouteRuleDeleteControl(selectedRouteRuleID);
    return selectedRouteRuleID;
  }

  function createRouteRuleEditor(rule) {
    var card = document.createElement("details");
    card.className = "route-rule-card";

    var summary = document.createElement("summary");
    summary.className = "route-rule-summary";
    var summaryTitle = document.createElement("span");
    summaryTitle.className = "route-rule-summary-title";
    summaryTitle.textContent = rule.name || "路由规则";
    var summaryPath = document.createElement("span");
    summaryPath.className = "route-rule-summary-path";
    summaryPath.textContent = rule.path || "";
    var summaryContent = document.createElement("span");
    summaryContent.className = "route-rule-summary-content";
    summaryContent.appendChild(summaryTitle);
    summaryContent.appendChild(summaryPath);
    summary.appendChild(summaryContent);
    card.appendChild(summary);

    var body = document.createElement("div");
    body.className = "route-rule-card-body";

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

    body.appendChild(form);
    card.appendChild(body);
    bindAjaxForm(form);
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
      var binary = window.atob(dataNode.dataset.rules);
      var bytes = Uint8Array.from(binary, function (character) {
        return character.charCodeAt(0);
      });
      return JSON.parse(new TextDecoder("utf-8").decode(bytes));
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
    closeNewRouteRuleDialog();
  }

  function openNewRouteRuleDialog() {
    var dialog = document.getElementById("new-route-rule-dialog");
    if (!dialog) {
      return;
    }
    if (typeof dialog.showModal === "function") {
      dialog.showModal();
      return;
    }
    dialog.setAttribute("open", "");
  }

  function closeNewRouteRuleDialog() {
    var dialog = document.getElementById("new-route-rule-dialog");
    if (!dialog) {
      return;
    }
    if (typeof dialog.close === "function" && dialog.open) {
      dialog.close();
      return;
    }
    dialog.removeAttribute("open");
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
          var preferredRouteRuleID = "";
          if (form.id === "new-route-rule-form") {
            preferredRouteRuleID = findAddedRouteRuleID(data.route_rules || []);
          }
          updateDashboard(data, preferredRouteRuleID);
          resetNewRouteRuleForm(form, data);
          showToast(data.message || "操作成功", false);
        })
        .catch(function (error) {
          var message = error && error.message ? error.message : "保存失败";
          showMessage("");
          showToast(message, true);
        });
    });
  }

  document.addEventListener("DOMContentLoaded", function () {
    document.querySelectorAll("form[data-ajax-form]").forEach(bindAjaxForm);
    var select = document.getElementById("route-rule-select");
    if (select) {
      selectedRouteRuleID = select.value || "";
      routeRulesState = loadInitialRouteRules();
      updateRouteRuleDeleteControl(selectedRouteRuleID);
      renderRouteRuleEditor(selectedRouteRuleID);
      select.addEventListener("change", function () {
        selectedRouteRuleID = select.value || "";
        updateRouteRuleDeleteControl(selectedRouteRuleID);
        renderRouteRuleEditor(selectedRouteRuleID);
      });
    }

    var openButton = document.getElementById("open-new-route-rule");
    if (openButton) {
      openButton.addEventListener("click", openNewRouteRuleDialog);
    }
    document.querySelectorAll("[data-close-route-rule-dialog]").forEach(function (button) {
      button.addEventListener("click", closeNewRouteRuleDialog);
    });
  });
})();
