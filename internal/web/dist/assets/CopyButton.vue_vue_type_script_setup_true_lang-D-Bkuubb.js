import{c as l,d as i,b as p,P as u,q as d,j as n,f as s,h as m,t as y,k as f,n as o}from"./index-KIMsMwHl.js";import{C as h}from"./check-i_dp6jR0.js";/**
 * @license lucide-vue-next v0.468.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const k=l("CopyIcon",[["rect",{width:"14",height:"14",x:"8",y:"8",rx:"2",ry:"2",key:"17jyea"}],["path",{d:"M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2",key:"zix9uf"}]]),v=i({__name:"CopyButton",props:{text:{},label:{},tone:{}},setup(a){const c=a,t=f(!1);async function r(){try{await navigator.clipboard.writeText(c.text)}catch{const e=document.createElement("textarea");e.value=c.text,document.body.appendChild(e),e.select(),document.execCommand("copy"),e.remove()}t.value=!0,window.setTimeout(()=>{t.value=!1},1500)}return(e,x)=>(o(),p("button",{class:d(["btn btn-sm",a.tone==="dark"?"copy-btn":""]),onClick:u(r,["stop"])},[t.value?(o(),n(s(h),{key:0,size:13})):(o(),n(s(k),{key:1,size:13})),m("span",null,y(t.value?"已复制":a.label||"复制"),1)],2))}});export{v as _};
