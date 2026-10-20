(function () {
  var routeRulesState = [];
  var selectedRouteRuleID = "";
  var toastSequence = 0;
  var singBoxPresets = {};
  var templateBaseline = "";

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

    var parent = document.querySelector("dialog[open]") || document.body;
    if (region.parentElement !== parent) { parent.appendChild(region); }

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
    setText("subscription-url", subscriptionDisplayURL(data.subscription_path, data.subscription_url));
    setText("subscription-path", data.subscription_path || "");
    setText("singbox-url", subscriptionDisplayURL(data.singbox_path, data.singbox_url));
    setText("singbox-path", data.singbox_path || "");
    renderSingBoxReport(data.singbox_report || {}, data.singbox_status_text || "");
    setText("nodes-count", String(data.node_count || 0));
    setText("route-rules-count", String((data.route_rules || []).length));
    setValue("path-input", data.subscription_path || "");
    setValue("nodes-textarea", data.nodes_text || "");
    renderRouteRules(data.route_rules || [], selected);
  }

  function subscriptionDisplayURL(path, fallback) {
    return path && window.location.origin ? window.location.origin + path : (fallback || "");
  }

  function renderSingBoxReport(report, statusText) {
    setText("singbox-status", statusText);
    var status = document.getElementById("singbox-status");
    if (status) { status.classList.toggle("conversion-error", report.status === "error" || report.status === "stale"); }
    setText("singbox-counts", "共 " + (report.total || 0) + " 个节点，可转换 " + (report.converted || 0) + " 个，失败 " + (report.failed || 0) + " 个，警告 " + (report.warnings || 0) + " 条");
    setText("singbox-generated-at", report.generated_at ? "生成时间：" + report.generated_at : "");
    var details = document.getElementById("singbox-diagnostics-details");
    var list = document.getElementById("singbox-diagnostics");
    var diagnostics = report.diagnostics || [];
    if (!details || !list) { return; }
    details.hidden = !diagnostics.length;
    list.innerHTML = "";
    diagnostics.forEach(function (diagnostic) {
      var item = document.createElement("li");
      item.textContent = (diagnostic.source_index ? "节点 " + diagnostic.source_index + "：" : "") + diagnostic.message + "（" + diagnostic.code + "）";
      list.appendChild(item);
    });
  }

  function copySubscriptionURL(button) {
    var target = document.getElementById(button.dataset.copyTarget);
    if (!target) { return; }
    var text = target.textContent;
    if (!text) { showToast("订阅链接暂时不可用", true); return; }
    if (window.isSecureContext && navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text).then(function () { showToast("订阅链接已复制", false); }).catch(function () { showToast("复制失败，请手动选择订阅链接", true); });
      return;
    }
    var field = document.createElement("textarea");
    field.className = "clipboard-field";
    field.value = text;
    document.body.appendChild(field);
    field.select();
    var copied = false;
    try { copied = document.execCommand("copy"); } catch (error) { copied = false; }
    field.remove();
    button.focus();
    showToast(copied ? "订阅链接已复制" : "复制失败，请手动选择订阅链接", !copied);
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

  function renderRouteRuleSelection(selectedID) {
    var rule = findRouteRule(routeRulesState, selectedID);
    setText("route-rule-url", rule ? subscriptionDisplayURL(rule.path, rule.url) : "");
    document.getElementById("open-edit-route-rule").disabled = !rule;
    document.getElementById("copy-route-rule-url").disabled = !rule;
  }

  function openEditRouteRuleDialog() {
    var rule = findRouteRule(routeRulesState, selectedRouteRuleID);
    if (!rule) { return; }
    var form = document.getElementById("edit-route-rule-form");
    if (form.dataset.saving === "true") { return; }
    form.setAttribute("action", "/route-rules/" + rule.id);
    form.dataset.ruleId = String(rule.id);
    setValue("route-rule-name-input", rule.name || "");
    setValue("route-rule-path-input", rule.path || "");
    setValue("route-rule-content-input", rule.content || "");
    setText("edit-route-rule-url", subscriptionDisplayURL(rule.path, rule.url));
    openDialog(document.getElementById("edit-route-rule-dialog"));
  }

  function renderRouteRules(routeRules, selectedID) {
    routeRulesState = routeRules.slice();
    renderRouteRulePaths(routeRulesState);
    var nextSelectedID = renderRouteRuleOptions(routeRulesState, selectedID);
    renderRouteRuleSelection(nextSelectedID);
  }

  function renderRouteRulePaths(routeRules) {
    var list = document.getElementById("route-rule-paths");
    if (!list) {
      return;
    }
    list.innerHTML = "";
    if (!routeRules.length) {
      var empty = document.createElement("li");
      empty.className = "muted";
      empty.textContent = "暂无路由规则";
      list.appendChild(empty);
      return;
    }
    routeRules.forEach(function (rule) {
      var item = document.createElement("li");
      var name = document.createElement("span");
      name.className = "muted";
      name.textContent = rule.name || "路由规则";
      var path = document.createElement("code");
      path.textContent = rule.path || "";
      item.appendChild(name);
      item.appendChild(path);
      list.appendChild(item);
    });
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
    closeDialog(form.closest("dialog"));
  }

  function bindAjaxForm(form) {
    if (form.dataset.bound === "true") {
      return;
    }
    form.dataset.bound = "true";
    form.addEventListener("submit", function (event) {
      event.preventDefault();
      if (form.dataset.saving === "true") { return; }
      if (form.dataset.confirm && !window.confirm(form.dataset.confirm)) {
        return;
      }

      var submitter = event.submitter;
      var formData = new FormData(form);
      if (submitter && submitter.name) {
        formData.set(submitter.name, submitter.value);
      }

      var action = form.getAttribute("action") || window.location.href;
      form.dataset.saving = "true";
      var buttons = Array.from(form.querySelectorAll("button[type='submit']"));
      buttons.forEach(function (button) { button.disabled = true; });

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
          } else if (form.id === "edit-route-rule-form") {
            preferredRouteRuleID = form.dataset.ruleId;
          }
          updateDashboard(data, preferredRouteRuleID);
          resetNewRouteRuleForm(form, data);
          if (form.id === "singbox-template-form") {
            templateBaseline = document.getElementById("singbox-template-textarea").value;
            setText("singbox-template-state", "已保存");
            setTemplateError("");
          } else if (form.id === "nodes-form" || form.id === "subscription-form" || form.id === "edit-route-rule-form") {
            closeDialog(form.closest("dialog"));
          }
          var report = data.singbox_report || {};
          var updatesSingBox = form.id === "nodes-form" || form.id === "singbox-template-form";
          var savedMessage = form.id === "singbox-template-form" ? "模板已保存" : "节点已保存";
          if (updatesSingBox && (report.status === "stale" || report.status === "error")) {
            showToast(savedMessage + "；sing-box 未同步，请查看转换明细", true);
          } else if (updatesSingBox && report.status === "warning" && report.failed) {
            showToast(savedMessage + "；sing-box 已跳过 " + report.failed + " 个不兼容节点", false);
          } else {
            showToast(data.message || "操作成功", false);
          }
        })
        .catch(function (error) {
          var message = error && error.message ? error.message : "保存失败";
          if (form.id === "singbox-template-form") { setTemplateError(message); }
          showMessage("");
          showToast(message, true);
        })
        .finally(function () {
          form.dataset.saving = "false";
          buttons.forEach(function (button) { button.disabled = false; });
          if (form.id === "delete-route-rule-form") { updateRouteRuleDeleteControl(selectedRouteRuleID); }
        });
    });
  }

  function openDialog(dialog) {
    if (!dialog) { return; }
    if (typeof dialog.showModal === "function") { dialog.showModal(); }
    else { dialog.setAttribute("open", ""); }
  }

  function closeDialog(dialog) {
    if (!dialog) { return; }
    var region = document.getElementById("toast-region");
    if (region && dialog.contains(region)) { document.body.appendChild(region); }
    if (typeof dialog.close === "function" && dialog.open) { dialog.close(); }
    else { dialog.removeAttribute("open"); }
  }

  function setTemplateError(message) {
    var node = document.getElementById("singbox-template-error");
    if (node) { node.textContent = message; node.hidden = !message; }
  }

  function openTemplateDialog() {
    openDialog(document.getElementById("singbox-template-dialog"));
    setText("singbox-template-state", "载入中");
    setTemplateError("");
    document.getElementById("singbox-template-textarea").disabled = true;
    document.getElementById("save-singbox-template").disabled = true;
    document.getElementById("load-singbox-preset").disabled = true;
    fetch("/api/singbox-template", {credentials:"same-origin", headers:{Accept:"application/json"}})
      .then(function (response) {
        return response.json().then(function (data) { if (!response.ok) { throw data; } return data; });
      })
      .then(function (data) {
        singBoxPresets = data.presets || {};
        templateBaseline = data.template || "";
        setValue("singbox-template-textarea", templateBaseline);
        setText("singbox-template-state", data.read_error ? "载入失败" : "已载入保存的模板");
        setTemplateError(data.read_error || "");
        document.getElementById("save-singbox-template").disabled = false;
        document.getElementById("load-singbox-preset").disabled = false;
      })
      .catch(function (error) { setTemplateError(error.message || "模板读取失败"); setText("singbox-template-state", "载入失败"); })
      .finally(function () { document.getElementById("singbox-template-textarea").disabled = false; });
  }

  document.addEventListener("DOMContentLoaded", function () {
    ["subscription", "singbox"].forEach(function (prefix) {
      var path = document.getElementById(prefix + "-path");
      var url = document.getElementById(prefix + "-url");
      if (path && url) { url.textContent = subscriptionDisplayURL(path.textContent, url.textContent); }
    });
    document.querySelectorAll("button[data-copy-target]").forEach(function (button) {
      button.addEventListener("click", function () { copySubscriptionURL(button); });
    });
    document.querySelectorAll("form[data-ajax-form]").forEach(bindAjaxForm);
    document.querySelectorAll("[data-open-dialog]").forEach(function (button) {
      button.addEventListener("click", function () { openDialog(document.getElementById(button.dataset.openDialog)); });
    });
    document.querySelectorAll("[data-close-dialog]").forEach(function (button) {
      button.addEventListener("click", function () {
        var dialog = button.closest("dialog");
        if (dialog.id === "singbox-template-dialog" && document.getElementById("singbox-template-textarea").value !== templateBaseline && !window.confirm("放弃未保存的模板修改？")) { return; }
        closeDialog(dialog);
      });
    });
    var templateButton = document.getElementById("open-singbox-template");
    if (templateButton) { templateButton.addEventListener("click", openTemplateDialog); }
    var presetButton = document.getElementById("load-singbox-preset");
    if (presetButton) { presetButton.addEventListener("click", function () {
      var preset = singBoxPresets[document.getElementById("singbox-preset").value];
      if (!preset) { return; }
      var field = document.getElementById("singbox-template-textarea");
      if (field.value !== templateBaseline && !window.confirm("用预设替换未保存的修改？")) { return; }
      field.value = preset;
      setText("singbox-template-state", "预设已载入，未保存");
      setTemplateError("");
    }); }
    var templateField = document.getElementById("singbox-template-textarea");
    if (templateField) { templateField.addEventListener("input", function () { setText("singbox-template-state", templateField.value === templateBaseline ? "已保存" : "未保存"); }); }
    var templateDialog = document.getElementById("singbox-template-dialog");
    if (templateDialog) { templateDialog.addEventListener("cancel", function (event) {
      if (templateField.value !== templateBaseline && !window.confirm("放弃未保存的模板修改？")) { event.preventDefault(); }
    }); }
    var select = document.getElementById("route-rule-select");
    if (select) {
      renderRouteRules(loadInitialRouteRules(), select.value || "");
      select.addEventListener("change", function () {
        selectedRouteRuleID = select.value || "";
        updateRouteRuleDeleteControl(selectedRouteRuleID);
        renderRouteRuleSelection(selectedRouteRuleID);
      });
    }

    var editButton = document.getElementById("open-edit-route-rule");
    if (editButton) { editButton.addEventListener("click", openEditRouteRuleDialog); }
  });
})();
